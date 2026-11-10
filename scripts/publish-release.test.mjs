/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { expect, test } from "bun:test";
import { createHash, generateKeyPairSync, sign } from "node:crypto";
import { mkdtemp, mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { publish, releasePlan, verifyAppleRelease } from "./publish-release.mjs";
import * as release from "./publish-release.mjs";

async function fixture(run) {
  const root = await mkdtemp(join(tmpdir(), "xsourcetree-publish-"));
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const base = "https://cdn.example.test/XSourceTree";
  try {
    await writeFile(join(root, "package.json"), JSON.stringify({ version: "1.2.3" }));
    await writeFile(join(root, "release.config.json"), JSON.stringify({
      downloadBase: base, storageTarget: "test/apps/XSourceTree",
      githubRepository: "ysicing/XSourceTree",
      publicKey: publicKey.export({ type: "spki", format: "der" }).subarray(-32).toString("base64"),
      macos: { teamID: "TEAM123456" },
    }));
    for (const arch of ["arm64", "amd64"]) {
      const target = `darwin-${arch}`;
      const folder = join(root, "build", target);
      await mkdir(folder, { recursive: true });
      const archive = Buffer.from(`signed test archive ${arch}`);
      const name = `xsourcetree-1.2.3-${target}.tar.gz`;
      await writeFile(join(folder, name), archive);
      await writeFile(join(folder, `xsourcetree-1.2.3-${arch}.dmg`), "test dmg");
      await writeFile(join(folder, `update-${target}.json`), JSON.stringify({
        version: "1.2.3", notes: "更新说明", size: archive.length, url: `${base}/${name}`,
        signature: sign(null, createHash("sha256").update(archive).digest(), privateKey).toString("base64"),
      }));
    }
    await run(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

function storage(plan, options = {}) {
  const events = [];
  return {
    events,
    async run(args, hashing) {
      events.push(args);
      const object = args.at(-1);
      if (args[0] === "stat") {
        if (options.statFailure) return { status: 1, text: "access denied" };
        if (options.existing || (options.currentVersion && plan.feeds.some((feed) => feed.object === object))) {
          return { status: 0 };
        }
        return { status: 1, text: JSON.stringify({
          status: "error", error: { cause: { message: "Object does not exist" } },
        }) };
      }
      if (args[0] === "cat") {
        const file = plan.artifacts.find((item) => item.object === object);
        return hashing ? { status: 0, hash: options.conflict ? "wrong" : file.hash } :
          { status: 0, text: JSON.stringify({ version: options.currentVersion ?? plan.version }) };
      }
      return { status: options.uploadFailure ? 1 : 0 };
    },
    async remoteHash(url) {
      events.push(["read", url]);
      return options.corrupt ? "wrong" : [...plan.artifacts, ...plan.feeds].find((file) => file.url === url).hash;
    },
  };
}

test("checks both architectures and rejects wrong signatures or missing packages", async () => {
  await fixture(async (root) => {
    expect((await releasePlan(root)).artifacts).toHaveLength(6);
    const archive = join(root, "build/darwin-amd64/xsourcetree-1.2.3-darwin-amd64.tar.gz");
    const original = await readFile(archive);
    await writeFile(archive, Buffer.alloc(original.length));
    await expect(releasePlan(root)).rejects.toThrow("签名");
    await rm(archive);
    await expect(releasePlan(root)).rejects.toThrow();
  });
});

test("publishes feeds only after all immutable objects pass CDN verification", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    const fake = storage(plan);
    await publish(plan, fake);
    const writes = fake.events.filter((args) => args.includes("cp"));
    expect(writes).toHaveLength(8);
    expect(writes.slice(0, 6).every((args) => args.includes("If-None-Match: *"))).toBe(true);
    const firstFeed = fake.events.findIndex((args) => args[0] === "cp");
    expect(fake.events.slice(0, firstFeed).filter((args) => args[0] === "read")).toHaveLength(6);
    expect(writes.at(-1)).toContain("Cache-Control=no-cache,max-age=0,must-revalidate");
  });
});

test("storage errors, existing conflicts, failed uploads and corrupt CDN never publish feeds", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const options of [
      { statFailure: true }, { existing: true, conflict: true }, { uploadFailure: true },
      { corrupt: true }, { currentVersion: "1.3.0" },
    ]) {
      const fake = storage(plan, options);
      await expect(publish(plan, fake)).rejects.toThrow();
      expect(fake.events.some((args) => args[0] === "cp")).toBe(false);
    }
  });
});

test("identical artifacts are safe to retry without overwriting version objects", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    const fake = storage(plan, { existing: true });
    await publish(plan, fake);
    expect(fake.events.filter((args) => args.includes("cp"))).toHaveLength(2);
  });
});

test("validates archived apps and disk images for both architectures", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    const calls = [];
    await verifyAppleRelease(root, "build", plan, (program, args) => {
      calls.push([program, ...args]);
      return {
        status: 0,
        stderr: "Authority=Developer ID Application: Test\nTeamIdentifier=TEAM123456\nCodeDirectory flags=0x10000(runtime)\n",
      };
    });
    const archives = calls.filter(([program]) => program === "tar");
    expect(archives).toHaveLength(2);
    expect(archives[0][2]).toEndWith("xsourcetree-1.2.3-darwin-arm64.tar.gz");
    expect(archives[1][2]).toEndWith("xsourcetree-1.2.3-darwin-amd64.tar.gz");
    const applications = calls.filter(([program, operation]) => program === "codesign" && operation === "--verify");
    expect(applications[0].at(-1)).toBe(join(archives[0].at(-1), "XSourceTree.app"));
    expect(calls.filter(([program]) => program === "xcrun")).toHaveLength(4);
    expect(calls.filter(([program]) => program === "spctl")).toHaveLength(4);
    await expect(stat(archives[0].at(-1))).rejects.toThrow();
  });
});

test("refuses ad-hoc, development, wrong-team and unhardened signatures", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const signature of [
      "Signature=adhoc\nTeamIdentifier=not set\n",
      "Authority=Apple Development: Test\nTeamIdentifier=TEAM123456\nflags=0x10000(runtime)\n",
      "Authority=Developer ID Application: Test\nTeamIdentifier=OTHER12345\nflags=0x10000(runtime)\n",
      "Authority=Developer ID Application: Test\nTeamIdentifier=TEAM123456\nflags=0x0(none)\n",
    ]) {
      await expect(verifyAppleRelease(root, "build", plan, () => ({
        status: 0, stderr: signature,
      }))).rejects.toThrow("Developer ID");
    }
  });
});

test("refuses failed extraction, signature, stapling and Gatekeeper checks", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const failing of ["tar", "codesign", "xcrun", "spctl"]) {
      await expect(verifyAppleRelease(root, "build", plan, (program) => ({
        status: program === failing ? 1 : 0,
        stderr: "Authority=Developer ID Application: Test\nTeamIdentifier=TEAM123456\nflags=0x10000(runtime)\n",
      }))).rejects.toThrow("Apple 发布校验失败");
    }
  });
});

function github(plan, options = {}) {
  const events = [];
  const packages = plan.artifacts.filter((file) => file.name.endsWith(".dmg"));
  let current = options.existing ? {
    tag_name: "v1.2.3", draft: options.draft ?? false, prerelease: false,
    assets: packages.slice(0, options.partial ? 1 : 2).map((file) => ({
      name: file.name, state: "uploaded", digest: `sha256:${file.hash}`,
    })),
  } : null;
  if (options.conflict) current.assets[0].digest = "sha256:wrong";
  if (options.unverified) current.assets[0].digest = null;
  return {
    events,
    get current() { return current; },
    run(program, args, settings) {
      expect(program).toBe("gh");
      events.push(args);
      const endpoint = args.at(-1);
      if (args[0] === "api" && endpoint.includes("/git/ref/tags/")) {
        return { status: options.missingTag ? 1 : 0, stderr: "tag unavailable" };
      }
      if (args[0] === "api") {
        if (options.lookupFailure) return { status: 1, stderr: "forbidden" };
        if (endpoint !== "repos/ysicing/XSourceTree/releases?per_page=100" ||
            !args.includes("--paginate") || !args.includes("--jq") || args.includes("--slurp")) {
          throw new Error("draft lookup must filter paginated releases before collecting output");
        }
        expect(args[args.indexOf("--jq") + 1]).toBe(
          '.[] | select(.tag_name == "v1.2.3") | {tag_name, draft, prerelease, assets: [.assets[] | {name, state, digest}]}',
        );
        return { status: 0, stdout: current ? JSON.stringify(current) : "" };
      }
      if (args[1] === "create") {
        expect(args).toContain("--verify-tag");
        expect(args).toContain("--draft");
        expect(settings.input).toBe("更新说明");
        current = { tag_name: args[2], draft: true, prerelease: false, assets: [] };
      } else if (args[1] === "upload") {
        expect(args).not.toContain("--clobber");
        if (options.uploadFailure) return { status: 1, stderr: "upload failed" };
        const file = packages.find((item) => args.includes(item.path));
        if (!file) throw new Error("unexpected release asset");
        if (options.starterFailure) {
          current.assets.push({ name: file.name, state: "starter", digest: null, size: 0 });
          return { status: 1, stderr: "HTTP 502 Bad Gateway" };
        }
        current.assets.push({
          name: file.name, state: "uploaded",
          digest: options.corrupt ? "sha256:wrong" : `sha256:${file.hash}`,
        });
      } else if (args[1] === "edit") {
        expect(args).toContain("--draft=false");
        if (!options.stuckDraft) current.draft = false;
      } else {
        throw new Error(`unexpected gh command: ${args.join(" ")}`);
      }
      return { status: 0, stdout: "" };
    },
  };
}

test("publishes both DMGs on the existing version tag after verifying uploaded digests", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    const fake = github(plan);
    await release.publishGitHubRelease(plan, fake);
    expect(fake.current.tag_name).toBe("v1.2.3");
    expect(fake.current.draft).toBe(false);
    expect(fake.current.assets.map((file) => file.name)).toEqual([
      "xsourcetree-1.2.3-arm64.dmg", "xsourcetree-1.2.3-amd64.dmg",
    ]);
    expect(fake.events.every((args) => args.includes("github.com") ||
      args.includes("github.com/ysicing/XSourceTree"))).toBe(true);
    const finalUpload = fake.events.findLastIndex((args) => args[1] === "upload");
    const publication = fake.events.findIndex((args) => args[1] === "edit");
    expect(fake.events.slice(finalUpload + 1, publication).some((args) => args[0] === "api")).toBe(true);
  });
});

test("resumes partial drafts and skips matching assets on already published releases", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const options of [
      { existing: true, draft: true, partial: true },
      { existing: true, partial: true }, { existing: true },
    ]) {
      const fake = github(plan, options);
      await release.publishGitHubRelease(plan, fake);
      expect(fake.current.assets).toHaveLength(2);
      expect(fake.current.draft).toBe(false);
      expect(fake.events.some((args) => args[1] === "create")).toBe(false);
      expect(fake.events.filter((args) => args[1] === "upload")).toHaveLength(options.partial ? 1 : 0);
      expect(fake.events.filter((args) => args[1] === "edit")).toHaveLength(options.draft ? 1 : 0);
    }
  });
});

test("refuses missing tags, unreadable releases and mismatched or unverifiable existing assets", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const options of [
      { missingTag: true }, { lookupFailure: true },
      { existing: true, conflict: true }, { existing: true, unverified: true },
    ]) {
      const fake = github(plan, options);
      await expect(release.publishGitHubRelease(plan, fake)).rejects.toThrow();
      expect(fake.events.every((args) => args[0] === "api")).toBe(true);
    }
  });
});

test("keeps releases in draft after failed uploads or digest verification", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    for (const options of [{ uploadFailure: true }, { corrupt: true }]) {
      const fake = github(plan, options);
      await expect(release.publishGitHubRelease(plan, fake)).rejects.toThrow();
      expect(fake.current.draft).toBe(true);
      expect(fake.events.some((args) => args[1] === "edit")).toBe(false);
    }
  });
});

test("rejects incomplete GitHub publication and invalid repository configuration", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    await expect(release.publishGitHubRelease(plan, github(plan, { stuckDraft: true }))).rejects.toThrow();
    const configPath = join(root, "release.config.json");
    const config = JSON.parse(await readFile(configPath, "utf8"));
    config.githubRepository = "../wrong/repository";
    await writeFile(configPath, JSON.stringify(config));
    await expect(releasePlan(root)).rejects.toThrow("GitHub");
  });
});

test("reports failed starter uploads and resumes after the operator removes only the placeholder", async () => {
  await fixture(async (root) => {
    const plan = await releasePlan(root);
    const options = { starterFailure: true };
    const fake = github(plan, options);
    await expect(release.publishGitHubRelease(plan, fake)).rejects.toThrow("502");
    const beforeRetry = fake.events.length;
    await expect(release.publishGitHubRelease(plan, fake)).rejects.toThrow("手动删除");
    expect(fake.events.slice(beforeRetry).every((args) => args[0] === "api")).toBe(true);
    expect(fake.current.assets).toHaveLength(1);
    expect(fake.current.assets[0].state).toBe("starter");
    expect(fake.current.draft).toBe(true);
    fake.current.assets.splice(0, 1);
    options.starterFailure = false;
    await release.publishGitHubRelease(plan, fake);
    expect(fake.current.assets).toHaveLength(2);
    expect(fake.current.draft).toBe(false);
  });
});
