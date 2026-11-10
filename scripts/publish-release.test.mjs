import { expect, test } from "bun:test";
import { createHash, generateKeyPairSync, sign } from "node:crypto";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { publish, releasePlan } from "./publish-release.mjs";

async function fixture(run) {
  const root = await mkdtemp(join(tmpdir(), "xsourcetree-publish-"));
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const base = "https://cdn.example.test/XSourceTree";
  try {
    await writeFile(join(root, "package.json"), JSON.stringify({ version: "1.2.3" }));
    await writeFile(join(root, "release.config.json"), JSON.stringify({
      downloadBase: base, storageTarget: "test/apps/XSourceTree",
      publicKey: publicKey.export({ type: "spki", format: "der" }).subarray(-32).toString("base64"),
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
