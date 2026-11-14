/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const platforms = ["windows-amd64", "linux-amd64"];

function invoke(root, run, program, args) {
  const result = run(program, args, {
    cwd: root, encoding: "utf8", timeout: 300_000, maxBuffer: 4 * 1024 * 1024,
    env: { ...process.env, GH_PROMPT_DISABLED: "1" },
  });
  if (result.error || result.status !== 0) {
    throw new Error(`${program} 失败：${result.error?.message ?? result.stderr ?? result.stdout}`);
  }
  return result.stdout.trim();
}

function assetName(version, platform) {
  if (!platforms.includes(platform)) throw new Error("仅支持 Windows amd64 和 Ubuntu amd64");
  return platform === "windows-amd64" ? `51code-setup-${version}-amd64.exe` : `51code_${version}_amd64.deb`;
}

export async function buildDesktop(root, platform, run = spawnSync) {
  assetName("0.0.0", platform);
  const targetOS = platform.split("-")[0];
  const { build } = await import("./build.mjs");
  return build(root, ["-platform", `${targetOS}/amd64`, "-o", "build/desktop"], (program, args, options) => run(program, args, {
    ...options, env: { ...options.env, MYGO_TARGET_OS: targetOS },
  }));
}

export async function sourceContext(root, tag = "", run = spawnSync) {
  const pkg = JSON.parse(await readFile(resolve(root, "package.json"), "utf8"));
  // 首版仅发布稳定版本，避免 Debian 和 SemVer 的预发行版本转换产生歧义。
  if (pkg.name !== "51code" || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(pkg.version)) {
    throw new Error("package.json 必须是 51code 的稳定版本");
  }
  if (tag && tag !== `v${pkg.version}`) throw new Error("tag 必须与 package.json 版本一致");
  const git = (...args) => invoke(root, run, "git", args);
  const commit = git("rev-parse", "--verify", "HEAD^{commit}");
  if (!/^[a-f0-9]{40}$/.test(commit)) throw new Error("当前提交无效");
  if (tag && git("rev-parse", "--verify", `refs/tags/${tag}^{commit}`) !== commit) {
    throw new Error("tag 未指向当前构建提交");
  }
  return { version: pkg.version, tag, commit };
}

export function publishedRelease(root, context, repository, run = spawnSync) {
  if (!context.tag || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository ?? "") ||
      repository.split("/").some((part) => part === "." || part === "..")) {
    throw new Error("发布必须提供 tag 和 GitHub repository");
  }
  const api = (path) => JSON.parse(invoke(root, run, "gh", ["api", "--hostname", "github.com", `repos/${repository}/${path}`]));
  const release = api(`releases/tags/${context.tag}`);
  if (release.tag_name !== context.tag || release.draft !== false || release.prerelease !== false ||
      !release.published_at || !Number.isSafeInteger(release.id) || !Array.isArray(release.assets)) {
    throw new Error("只允许向已公开发布的稳定 GitHub Release 添加附件");
  }
  // target_commitish 可能只是分支名；必须从远端 tag 解引用到实际提交。
  let object = api(`git/ref/tags/${context.tag}`).object;
  for (let depth = 0; object?.type === "tag" && depth < 8; depth++) {
    if (!/^[a-f0-9]{40}$/.test(object.sha)) throw new Error("远端 tag 对象无效");
    object = api(`git/tags/${object.sha}`).object;
  }
  if (object?.type !== "commit" || object.sha !== context.commit) {
    throw new Error("远端 tag 未指向当前构建提交");
  }
  return release;
}

async function fileInfo(path) {
  const info = await lstat(path);
  if (!info.isFile() || info.size === 0) throw new Error(`安装包必须是非空普通文件：${path}`);
  const bytes = await readFile(path);
  return { size: bytes.length, hash: createHash("sha256").update(bytes).digest("hex") };
}

export async function collect(root, platform, { tag = "", repository, run = spawnSync, directory = "build/desktop" } = {}) {
  const context = await sourceContext(root, tag, run);
  const release = tag ? publishedRelease(root, context, repository, run) : null;
  const name = assetName(context.version, platform);
  const folder = resolve(root, directory, platform);
  const file = await fileInfo(resolve(folder, name));
  const record = { schema: 1, ...context, releaseID: release?.id ?? null, platform, name, ...file };
  await writeFile(resolve(folder, "desktop-artifact.json"), JSON.stringify(record, null, 2) + "\n");
  return record;
}

export async function releasePlan(root, context, release, directory = "build/desktop") {
  const artifacts = [];
  for (const platform of platforms) {
    const folder = resolve(root, directory, platform);
    const record = JSON.parse(await readFile(resolve(folder, "desktop-artifact.json"), "utf8"));
    const name = assetName(context.version, platform);
    if (record.schema !== 1 || record.version !== context.version || record.tag !== context.tag ||
        record.commit !== context.commit || record.releaseID !== release.id || record.platform !== platform || record.name !== name) {
      throw new Error(`${platform} 安装包来源与本次 Release 不一致`);
    }
    const path = resolve(folder, name);
    const file = await fileInfo(path);
    if (record.size !== file.size || record.hash !== file.hash) throw new Error(`${name} 的 SHA-256 或大小不一致`);
    artifacts.push({ name, path, ...file });
  }
  return artifacts;
}

function verifyAsset(file, asset) {
  if (asset.state !== "uploaded" || asset.size !== file.size || asset.digest !== `sha256:${file.hash}`) {
    throw new Error(`${file.name} 已存在但 SHA-256、大小或状态无法验证；禁止覆盖或删除，请核实现有附件`);
  }
}

function existingAsset(release, file) {
  const matches = release.assets.filter((asset) => asset.name === file.name);
  if (matches.length > 1) throw new Error(`${file.name} 存在重复附件`);
  if (matches.length) verifyAsset(file, matches[0]);
  return matches.length === 1;
}

export async function publish(root, { tag, repository, run = spawnSync, directory = "build/desktop" } = {}) {
  const context = await sourceContext(root, tag, run);
  const release = publishedRelease(root, context, repository, run);
  const files = await releasePlan(root, context, release, directory);
  // 全部预检通过后再上传，已有相同内容可重试；不使用 --clobber，竞争上传也不得覆盖。
  const pending = files.filter((file) => !existingAsset(release, file));
  for (const file of pending) {
    const label = file.name.endsWith(".exe") ? "Windows x64（未提供 Authenticode 签名，手动升级）" : "Ubuntu 24.04 x64（deb，手动升级）";
    invoke(root, run, "gh", ["release", "upload", context.tag, `${file.path}#${label}`, "--repo", `github.com/${repository}`]);
  }
  const after = publishedRelease(root, context, repository, run);
  if (after.id !== release.id) throw new Error("Release 在上传期间被替换");
  for (const file of files) {
    if (!existingAsset(after, file)) throw new Error(`${file.name} 上传后不存在`);
  }
  return files.map((file) => file.name);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const [command, ...args] = process.argv.slice(2);
    const options = { repository: process.env.GITHUB_REPOSITORY };
    for (let i = 0; i < args.length; i += 2) {
      if (!["--tag", "--platform"].includes(args[i]) || !args[i + 1] || args[i + 1].startsWith("--")) {
        throw new Error("用法：desktop-release.mjs check|build|collect|publish [--tag vX.Y.Z] [--platform windows-amd64|linux-amd64]");
      }
      options[args[i].slice(2)] = args[i + 1];
    }
    const root = fileURLToPath(new URL("../", import.meta.url));
    if (command === "check") {
      const context = await sourceContext(root, options.tag);
      publishedRelease(root, context, options.repository);
      console.log(`已核实 ${context.tag} · ${context.commit}`);
    } else if (command === "build") {
      await buildDesktop(root, options.platform);
    } else if (command === "collect") {
      console.log(JSON.stringify(await collect(root, options.platform, options)));
    } else if (command === "publish") {
      console.log(`已核实发布附件：${(await publish(root, options)).join(", ")}`);
    } else {
      throw new Error("命令必须是 check、build、collect 或 publish");
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
