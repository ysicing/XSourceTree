/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { expect, test } from "bun:test";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";
import { buildDesktop, collect, platforms, publish, publishedRelease, releasePlan, sourceContext } from "./desktop-release.mjs";

const repository = "owner/51code";
const tag = "v1.2.3";

async function fixture(check) {
  const root = await mkdtemp(join(tmpdir(), "51code-desktop-"));
  try {
    await writeFile(join(root, "package.json"), JSON.stringify({ name: "51code", version: "1.2.3" }));
    const hooks = join(root, "empty-hooks");
    await mkdir(hooks);
    const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
    git("init", "-q");
    git("add", "package.json");
    git("-c", `core.hooksPath=${hooks}`, "-c", "commit.gpgsign=false", "-c", "user.name=Desktop Test", "-c", "user.email=desktop@example.test", "commit", "-qm", "fixture");
    git("-c", "tag.gpgsign=false", "tag", tag);
    const context = await sourceContext(root, tag);
    const release = { id: 42, tag_name: tag, draft: false, prerelease: false, published_at: "2026-11-14T00:00:00Z", assets: [] };
    const calls = [];
    const fake = {
      root, git, context, release, calls,
      remote: { type: "commit", sha: context.commit },
      annotated: false,
      failUpload: false,
      corruptUpload: false,
      replaceRelease: false,
      async records() {
        for (const platform of platforms) {
          const folder = join(root, "build/desktop", platform);
          await mkdir(folder, { recursive: true });
          const name = platform === "windows-amd64" ? "51code-setup-1.2.3-amd64.exe" : "51code_1.2.3_amd64.deb";
          await writeFile(join(folder, name), `installer for ${platform}`);
          const record = await collect(root, platform, { tag, repository, run });
          fake[platform] = record;
        }
      },
    };
    const run = (program, args, options) => {
      if (program === "git") return spawnSync(program, args, options);
      calls.push(args);
      const ok = (value) => ({ status: 0, stdout: JSON.stringify(value) });
      if (args[0] === "api") {
        if (args.at(-1) === `repos/${repository}/releases/tags/${tag}`) return ok(release);
        if (args.at(-1) === `repos/${repository}/git/ref/tags/${tag}`) {
          return ok({ object: fake.annotated ? { type: "tag", sha: "a".repeat(40) } : fake.remote });
        }
        if (args.at(-1) === `repos/${repository}/git/tags/${"a".repeat(40)}`) return ok({ object: fake.remote });
      }
      if (args[0] === "release" && args[1] === "upload") {
        if (fake.failUpload) return { status: 1, stderr: "upload failed" };
        const name = basename(args[3].split("#")[0]);
        const file = platforms.map((p) => fake[p]).find((r) => r.name === name);
        release.assets.push({ name, state: "uploaded", size: file.size, digest: `sha256:${fake.corruptUpload ? "0".repeat(64) : file.hash}` });
        if (fake.replaceRelease) release.id++;
        return { status: 0, stdout: "" };
      }
      throw new Error(`Unexpected gh call: ${JSON.stringify(args)}`);
    };
    fake.run = run;
    await check(fake);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

const uploads = (fake) => fake.calls.filter((args) => args[0] === "release");
const options = (fake) => ({ tag, repository, run: fake.run });

function existing(record, changes = {}) {
  return { name: record.name, state: "uploaded", size: record.size, digest: `sha256:${record.hash}`, ...changes };
}

test("injects the build target into the real pinned CLI invocation without requiring signing keys", async () => {
  await fixture(async (f) => {
    for (const platform of platforms) {
      let calls = 0;
      await buildDesktop(f.root, platform, (_program, args, options) => {
        calls++;
        const target = platform.split("-")[0];
        expect(args).toEqual(["build", "-platform", `${target}/amd64`, "-o", "build/desktop"]);
        expect(options.env.MYGO_TARGET_OS).toBe(target);
        expect(options.env.MYGO_LDFLAGS).toContain("-X main.buildCommit=");
        return { status: 0 };
      });
      expect(calls).toBe(1);
    }
    await expect(buildDesktop(f.root, "linux-arm64")).rejects.toThrow("仅支持");
  });
});

test("validates stable package version, tag and both lightweight and annotated local tags", async () => {
  await fixture(async (f) => {
    expect((await sourceContext(f.root, tag)).commit).toBe(f.context.commit);
    f.git("tag", "-d", tag);
    f.git("-c", "tag.gpgsign=false", "tag", "-a", tag, "-m", "fixture");
    expect((await sourceContext(f.root, tag)).commit).toBe(f.context.commit);
    await expect(sourceContext(f.root, "v1.2.4")).rejects.toThrow("版本一致");
    await writeFile(join(f.root, "package.json"), JSON.stringify({ name: "other", version: "1.2.3" }));
    await expect(sourceContext(f.root, tag)).rejects.toThrow("稳定版本");
    for (const version of ["1.2.3-rc.1", "01.2.3", "../1.2.3", "1.2", ""]) {
      await writeFile(join(f.root, "package.json"), JSON.stringify({ name: "51code", version }));
      await expect(sourceContext(f.root, tag)).rejects.toThrow("稳定版本");
    }
  });
});

test("refuses a local tag pointing at another build commit", async () => {
  await fixture(async (f) => {
    f.git("-c", "commit.gpgsign=false", "-c", "user.name=Desktop Test", "-c", "user.email=desktop@example.test", "commit", "--allow-empty", "-qm", "next");
    await expect(sourceContext(f.root, tag)).rejects.toThrow("当前构建提交");
  });
});

test("checks published stable Release and resolves remote annotated tags instead of target_commitish", async () => {
  await fixture(async (f) => {
    f.annotated = true;
    f.release.target_commitish = "main";
    expect(publishedRelease(f.root, f.context, repository, f.run).id).toBe(42);
    for (const patch of [{ draft: true }, { prerelease: true }, { published_at: null }, { tag_name: "v1.2.4" }]) {
      const saved = { ...f.release };
      Object.assign(f.release, patch);
      expect(() => publishedRelease(f.root, f.context, repository, f.run)).toThrow("已公开发布");
      Object.assign(f.release, saved);
    }
    f.remote = { type: "commit", sha: "b".repeat(40) };
    expect(() => publishedRelease(f.root, f.context, repository, f.run)).toThrow("当前构建提交");
    expect(() => publishedRelease(f.root, f.context, "../invalid", f.run)).toThrow("repository");
    expect(() => publishedRelease(f.root, { ...f.context, tag: "" }, repository, f.run)).toThrow("tag");
    expect(() => publishedRelease(f.root, f.context, repository, () => ({ status: 1, stderr: "404" }))).toThrow("404");
  });
});

test("collects only the two allowed installers and never publishes other build output", async () => {
  await fixture(async (f) => {
    await f.records();
    await writeFile(join(f.root, "build/desktop/windows-amd64", "update-windows-amd64.json"), "must stay local");
    await writeFile(join(f.root, "build/desktop/linux-amd64", "51code-1.2.3-linux-amd64.tar.gz"), "must stay local");
    expect(await publish(f.root, options(f))).toEqual([f[platforms[0]].name, f[platforms[1]].name]);
    expect(uploads(f)).toHaveLength(2);
    for (const args of uploads(f)) {
      expect(args.slice(0, 3)).toEqual(["release", "upload", tag]);
      expect(args).not.toContain("--clobber");
      expect(args).not.toContain("create");
      expect(args[3]).not.toContain("update-");
    }
    expect(uploads(f)[0][3]).toContain("未提供 Authenticode 签名");
    f.calls.length = 0;
    await publish(f.root, options(f));
    expect(uploads(f)).toHaveLength(0);
  });
});

test("refuses missing, empty and tampered installers or unapproved platforms", async () => {
  await fixture(async (f) => {
    await f.records();
    await expect(collect(f.root, "darwin-arm64")).rejects.toThrow("仅支持");
    const path = join(f.root, "build/desktop/linux-amd64", f["linux-amd64"].name);
    await writeFile(path, "different bytes");
    await expect(publish(f.root, options(f))).rejects.toThrow("SHA-256");
    await writeFile(path, "");
    await expect(collect(f.root, "linux-amd64")).rejects.toThrow("非空普通文件");
    await rm(path);
    await expect(publish(f.root, options(f))).rejects.toThrow();
    expect(uploads(f)).toHaveLength(0);
  });
});

test("rejects artifacts from a different tag, source, Release, version or filename", async () => {
  await fixture(async (f) => {
    await f.records();
    const path = join(f.root, "build/desktop/linux-amd64/desktop-artifact.json");
    const original = f["linux-amd64"];
    for (const patch of [{ commit: "b".repeat(40) }, { tag: "v1.2.4" }, { version: "1.2.4" }, { releaseID: 43 }, { platform: "linux-arm64" }, { name: "../other.deb" }, { schema: 2 }, { hash: "bad" }, { size: 0 }]) {
      await writeFile(path, JSON.stringify({ ...original, ...patch }));
      await expect(releasePlan(f.root, f.context, f.release)).rejects.toThrow();
    }
    await writeFile(path, JSON.stringify(original));
    f.remote = { type: "commit", sha: "b".repeat(40) };
    await expect(publish(f.root, options(f))).rejects.toThrow("当前构建提交");
    expect(uploads(f)).toHaveLength(0);
  });
});

test("checks every existing asset before uploading and never overwrites unverifiable content", async () => {
  await fixture(async (f) => {
    await f.records();
    const file = f["linux-amd64"];
    for (const patch of [{ digest: "sha256:bad" }, { digest: null }, { state: "starter" }, { size: 0 }]) {
      f.release.assets = [existing(file, patch)];
      await expect(publish(f.root, options(f))).rejects.toThrow("禁止覆盖");
      expect(uploads(f)).toHaveLength(0);
    }
    f.release.assets = [existing(file), existing(file)];
    await expect(publish(f.root, options(f))).rejects.toThrow("重复附件");
    expect(uploads(f)).toHaveLength(0);
  });
});

test("supports resuming with one identical asset and reports upload failures", async () => {
  await fixture(async (f) => {
    await f.records();
    f.release.assets = [existing(f["windows-amd64"])];
    f.failUpload = true;
    await expect(publish(f.root, options(f))).rejects.toThrow("upload failed");
    f.failUpload = false;
    f.calls.length = 0;
    await publish(f.root, options(f));
    expect(uploads(f)).toHaveLength(1);
    expect(uploads(f)[0][3]).toContain("51code_1.2.3_amd64.deb");
  });
});

test("fails post-upload verification on corrupt remote digest or replaced Release", async () => {
  for (const mode of ["corruptUpload", "replaceRelease"]) {
    await fixture(async (f) => {
      await f.records();
      f[mode] = true;
      await expect(publish(f.root, options(f))).rejects.toThrow();
    });
  }
});

test("branch build provenance cannot be uploaded as a release artifact", async () => {
  await fixture(async (f) => {
    await f.records();
    await collect(f.root, "windows-amd64");
    await expect(publish(f.root, options(f))).rejects.toThrow("来源");
    expect(uploads(f)).toHaveLength(0);
  });
});

test("selects updates by explicit build target and preserves original macOS settings", async () => {
  const root = fileURLToPath(new URL("../", import.meta.url));
  const release = JSON.parse(await readFile(join(root, "release.config.json"), "utf8"));
  const load = (target) => {
    const result = spawnSync(process.execPath, ["-e", 'import config from "./mygo.config.ts"; console.log(JSON.stringify(config));'], {
      cwd: root, encoding: "utf8", timeout: 30_000,
      env: { ...process.env, MYGO_TARGET_OS: target, XSOURCETREE_RELEASE: "1", XSOURCETREE_BUILD_NUMBER: "7", XSOURCETREE_BUILD_COMMIT: "abc1234", XSOURCETREE_BUILD_TIME: "2026-11-14T00:00:00Z" },
    });
    if (result.error || result.status !== 0) throw new Error(result.error?.message ?? result.stderr);
    return JSON.parse(result.stdout);
  };
  const mac = load("darwin");
  expect(mac.updates).toEqual({ publicKey: release.publicKey, url: release.downloadBase, deltas: 0 });
  expect(mac.macos).toEqual({
    minimumSystemVersion: "12.0", signingIdentity: release.macos.signingIdentity,
    notarize: { keychainProfile: release.macos.notaryProfile },
    infoPlist: { CFBundleVersion: "7", XSourceTreeCommit: "abc1234", XSourceTreeBuildTime: "2026-11-14T00:00:00Z" },
  });
  for (const target of ["windows", "linux"]) {
    const config = load(target);
    expect(config.updates).toBeUndefined();
    expect(config.windows?.certificate).toBeUndefined();
    expect(config.windows?.signCommand).toBeUndefined();
    expect(config.macos).toEqual(mac.macos);
    expect(config.linux).toEqual({ formats: ["deb"], webview: false, command: "51code", categories: ["Development"], depends: ["git", "libsecret-1-0"] });
  }
  expect(load("").updates).toEqual(process.platform === "darwin" ? mac.updates : undefined);
  expect(() => load("unsupported")).toThrow("不支持");
});
