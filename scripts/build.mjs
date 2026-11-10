/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { execFileSync, spawnSync } from "node:child_process";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { binaryPath } from "mygo-cli";

export async function build(root, args = [], run = spawnSync) {
  const state = resolve(root, ".mygo");
  const lock = resolve(state, "build.lock");
  await mkdir(state, { recursive: true });
  try {
    await mkdir(lock);
  } catch (error) {
    if (error.code === "EEXIST") throw new Error("已有构建进行中，请等待其结束；若构建曾被强制终止，请确认没有构建进程后清理 .mygo/build.lock");
    throw error;
  }
  try {
    const path = resolve(state, "build-info.json");
    let previous = 0;
    try {
      const info = JSON.parse(await readFile(path, "utf8"));
      if (!Number.isSafeInteger(info.number) || info.number < 1) throw new Error("构建号记录无效");
      previous = info.number;
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (!Number.isSafeInteger(previous + 1)) throw new Error("构建号超出有效范围");
    const git = (...parameters) => execFileSync("git", parameters, {
      cwd: root, encoding: "utf8", timeout: 30_000, stdio: ["ignore", "pipe", "pipe"],
    }).trim();
    const { version } = JSON.parse(await readFile(resolve(root, "package.json"), "utf8"));
    const info = {
      version,
      number: previous + 1,
      commit: git("rev-parse", "HEAD"),
      shortCommit: git("rev-parse", "--short=7", "HEAD"),
      dirty: git("status", "--porcelain", "--untracked-files=normal") !== "",
      time: new Date().toISOString(),
    };
    const commit = info.shortCommit + (info.dirty ? "-dirty" : "");
    const flags = [
      process.env.MYGO_LDFLAGS ?? "",
      `-X main.buildNumber=${info.number}`,
      `-X main.buildCommit=${commit}`,
      `-X main.buildTime=${info.time}`,
    ].filter(Boolean).join(" ");
    console.log(`构建 v${version} (Build ${info.number}) · ${commit} · ${info.time}`);
    const result = run(binaryPath(), ["build", ...args], {
      cwd: root, stdio: "inherit",
      env: {
        ...process.env,
        MYGO_LDFLAGS: flags,
        XSOURCETREE_BUILD_NUMBER: String(info.number),
        XSOURCETREE_BUILD_COMMIT: commit,
        XSOURCETREE_BUILD_TIME: info.time,
      },
    });
    if (result.error || result.status !== 0) {
      throw new Error(`构建失败，未递增构建号${result.error ? `：${result.error.message}` : ""}`);
    }
    await writeFile(`${path}.tmp`, JSON.stringify(info, null, 2) + "\n");
    await rename(`${path}.tmp`, path);
    return info;
  } finally {
    await rm(lock, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2);
    if (args[0] === "--") args.shift();
    if (args.includes("-h") || args.includes("--help")) {
      const result = spawnSync(binaryPath(), ["build", ...args], { stdio: "inherit" });
      if (result.error) throw result.error;
      process.exitCode = result.status ?? 1;
    } else {
      await build(fileURLToPath(new URL("../", import.meta.url)), args);
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
