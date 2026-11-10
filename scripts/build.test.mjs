import { expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { build } from "./build.mjs";

async function fixture(run) {
  const root = await mkdtemp(join(tmpdir(), "xsourcetree-build-"));
  try {
    await writeFile(join(root, "package.json"), JSON.stringify({ version: "1.2.3" }));
    await writeFile(join(root, ".gitignore"), ".mygo/\n");
    const git = (...args) => execFileSync("git", args, {
      cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"],
    }).trim();
    git("init", "-q");
    git("add", ".");
    git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false",
      "-c", "user.name=Build Test", "-c", "user.email=build@example.test", "commit", "-qm", "fixture");
    await run(root, git);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

test("successful builds increment persistently without changing the release version", async () => {
  await fixture(async (root) => {
    const first = await build(root, [], () => ({ status: 0 }));
    const second = await build(root, [], () => ({ status: 0 }));
    expect(first.number).toBe(1);
    expect(second.number).toBe(2);
    expect(first.dirty).toBe(false);
    expect(second.dirty).toBe(false);
    expect(JSON.parse(await readFile(join(root, ".mygo/build-info.json"), "utf8"))).toEqual(second);
    expect(JSON.parse(await readFile(join(root, "package.json"), "utf8")).version).toBe("1.2.3");
  });
});

test("both architectures receive the same build number, source commit and time", async () => {
  await fixture(async (root, git) => {
    const args = ["-platform", "darwin/arm64,darwin/amd64", "-o", "build/release-test"];
    let calls = 0;
    const info = await build(root, args, (_program, parameters, options) => {
      calls++;
      expect(parameters).toEqual(["build", ...args]);
      expect(options.cwd).toBe(root);
      expect(options.env.XSOURCETREE_BUILD_NUMBER).toBe("1");
      expect(options.env.XSOURCETREE_BUILD_COMMIT).toBe(git("rev-parse", "--short=7", "HEAD"));
      const time = options.env.XSOURCETREE_BUILD_TIME;
      expect(new Date(time).toISOString()).toBe(time);
      expect(options.env.MYGO_LDFLAGS).toContain("-X main.buildNumber=1");
      expect(options.env.MYGO_LDFLAGS).toContain(`-X main.buildTime=${time}`);
      expect(options.env.MYGO_LDFLAGS).toContain(`-X main.buildCommit=${options.env.XSOURCETREE_BUILD_COMMIT}`);
      return { status: 0 };
    });
    expect(calls).toBe(1);
    expect(info.commit).toBe(git("rev-parse", "HEAD"));
    expect(info.shortCommit).toBe(git("rev-parse", "--short=7", "HEAD"));
  });
});

test("failed builds keep the previous counter and release the build lock", async () => {
  await fixture(async (root) => {
    const first = await build(root, [], () => ({ status: 0 }));
    for (const failure of [{ status: 1 }, { status: null, error: new Error("spawn failed") }]) {
      await expect(build(root, [], () => failure)).rejects.toThrow("未递增构建号");
      expect(JSON.parse(await readFile(join(root, ".mygo/build-info.json"), "utf8"))).toEqual(first);
    }
    expect((await build(root, [], () => ({ status: 0 }))).number).toBe(2);
  });
});

test("uncommitted files are marked without losing the source commit", async () => {
  await fixture(async (root, git) => {
    await writeFile(join(root, "uncommitted.txt"), "work in progress");
    const info = await build(root, [], (_program, _args, { env }) => {
      expect(env.XSOURCETREE_BUILD_COMMIT).toBe(git("rev-parse", "--short=7", "HEAD") + "-dirty");
      return { status: 0 };
    });
    expect(info.dirty).toBe(true);
    expect(info.commit).toBe(git("rev-parse", "HEAD"));
  });
});

test("corrupt counters fail instead of silently reusing old build numbers", async () => {
  await fixture(async (root) => {
    await build(root, [], () => ({ status: 0 }));
    for (const invalid of ["invalid json", '{"number":0}', '{"number":1.5}', '{"number":9007199254740991}']) {
      await writeFile(join(root, ".mygo/build-info.json"), invalid);
      let called = false;
      await expect(build(root, [], () => {
        called = true;
        return { status: 0 };
      })).rejects.toThrow();
      expect(called).toBe(false);
      expect(await readFile(join(root, ".mygo/build-info.json"), "utf8")).toBe(invalid);
    }
  });
});

test("an existing build lock blocks another invocation and remains owned by the first", async () => {
  await fixture(async (root) => {
    const lock = join(root, ".mygo/build.lock");
    await mkdir(lock, { recursive: true });
    let calls = 0;
    const run = () => {
      calls++;
      return { status: 0 };
    };
    await expect(build(root, [], run)).rejects.toThrow("已有构建进行中");
    expect(calls).toBe(0);
    expect((await stat(lock)).isDirectory()).toBe(true);
    await rm(lock, { recursive: true });
    expect((await build(root, [], run)).number).toBe(1);
    expect(calls).toBe(1);
  });
});
