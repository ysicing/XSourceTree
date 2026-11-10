import { createHash, createPublicKey, verify } from "node:crypto";
import { mkdir, mkdtemp, readFile } from "node:fs/promises";
import { spawn, spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const sha256 = (data) => createHash("sha256").update(data).digest("hex");

export async function releasePlan(root, directory = "build") {
  const { version } = JSON.parse(await readFile(resolve(root, "package.json"), "utf8"));
  const config = JSON.parse(await readFile(resolve(root, "release.config.json"), "utf8"));
  const base = new URL(config.downloadBase);
  if (!/^\d+\.\d+\.\d+$/.test(version) || base.protocol !== "https:" ||
      base.username || base.password || base.search || base.hash ||
      !/^[\w.-]+\/[\w./-]+$/.test(config.storageTarget) ||
      config.storageTarget.split("/").some((part) => part === "." || part === "..")) {
    throw new Error("发布版本必须为稳定版，下载地址须为 HTTPS，存储目标须为 mc 别名下的路径");
  }
  const downloadBase = base.href.replace(/\/$/, "");
  const storageTarget = config.storageTarget.replace(/\/$/, "");
  const publicKey = Buffer.from(config.publicKey, "base64");
  if (publicKey.length !== 32) throw new Error("更新公钥无效");
  const key = createPublicKey({
    key: Buffer.concat([Buffer.from("302a300506032b6570032100", "hex"), publicKey]),
    format: "der", type: "spki",
  });
  const artifacts = [];
  const feeds = [];
  for (const arch of ["arm64", "amd64"]) {
    const target = `darwin-${arch}`;
    const folder = resolve(root, directory, target);
    const manifestName = `update-${target}.json`;
    const manifestPath = resolve(folder, manifestName);
    const manifestData = await readFile(manifestPath);
    const manifest = JSON.parse(manifestData);
    const archiveName = `xsourcetree-${version}-${target}.tar.gz`;
    const archivePath = resolve(folder, archiveName);
    const archive = await readFile(archivePath);
    const signature = Buffer.from(manifest.signature ?? "", "base64");
    if (manifest.version !== version || manifest.url !== `${downloadBase}/${archiveName}` ||
        manifest.size !== archive.length || !manifest.notes?.trim() || manifest.deltas?.length ||
        signature.length !== 64 ||
        !verify(null, createHash("sha256").update(archive).digest(), key, signature)) {
      throw new Error(`${target} 的清单、签名或更新包不一致，请用当前配置和签名私钥重新构建`);
    }
    const dmgName = `xsourcetree-${version}-${arch}.dmg`;
    const dmgPath = resolve(folder, dmgName);
    const dmg = await readFile(dmgPath);
    if (!dmg.length) throw new Error(`${dmgName} 为空`);
    const artifact = (name, path, data) => ({
      name, path, hash: sha256(data),
      object: `${storageTarget}/${name}`,
      url: `${downloadBase}/${name}`,
    });
    artifacts.push(
      artifact(archiveName, archivePath, archive),
      artifact(dmgName, dmgPath, dmg),
      artifact(`${version}/${manifestName}`, manifestPath, manifestData),
    );
    feeds.push({ ...artifact(manifestName, manifestPath, manifestData), version });
  }
  return { version, artifacts, feeds };
}

async function command(args, hashOutput = false) {
  return new Promise((resolveResult, reject) => {
    const child = spawn("mc", args, { stdio: ["ignore", "pipe", "ignore"], timeout: 300_000 });
    const digest = createHash("sha256");
    const chunks = [];
    let size = 0;
    child.stdout.on("data", (chunk) => {
      if (hashOutput) digest.update(chunk);
      else if ((size += chunk.length) <= 256 * 1024) chunks.push(chunk);
      else child.kill();
    });
    child.on("error", () => reject(new Error("无法运行 mc，请先安装并配置对象存储别名")));
    child.on("close", (status) => resolveResult({
      status, text: Buffer.concat(chunks).toString(), hash: digest.digest("hex"),
    }));
  });
}

async function onlineHash(url) {
  const response = await fetch(url, { cache: "no-store", signal: AbortSignal.timeout(300_000) });
  if (!response.ok) throw new Error(`CDN 回读失败：${url}（HTTP ${response.status}）`);
  const digest = createHash("sha256");
  for await (const chunk of response.body) digest.update(chunk);
  return digest.digest("hex");
}

function missing(result) {
  try {
    const state = JSON.parse(result.text);
    return state.status === "error" && state.error?.cause?.message === "Object does not exist";
  } catch {
    return false;
  }
}

function newer(left, right) {
  const first = left.split(".").map(Number);
  const second = right.split(".").map(Number);
  for (let index = 0; index < 3; index++) {
    if (first[index] !== second[index]) return first[index] > second[index];
  }
  return false;
}

export async function publish(plan, { run = command, remoteHash = onlineHash } = {}) {
  const existing = new Set();
  for (const file of plan.artifacts) {
    const stat = await run(["stat", "--json", file.object]);
    if (stat.status === 0) {
      const current = await run(["cat", file.object], true);
      if (current.status !== 0 || current.hash !== file.hash) {
        throw new Error(`禁止覆盖已发布的 ${file.name}；构建变化须推进版本号`);
      }
      existing.add(file.object);
    } else if (!missing(stat)) {
      throw new Error(`无法确认 ${file.name} 的源站状态，停止发布`);
    }
  }
  for (const feed of plan.feeds) {
    const stat = await run(["stat", "--json", feed.object]);
    if (stat.status === 0) {
      const current = await run(["cat", feed.object]);
      if (current.status !== 0) throw new Error("读取当前更新清单失败");
      const { version } = JSON.parse(current.text);
      if (!/^\d+\.\d+\.\d+$/.test(version) || newer(version, plan.version)) {
        throw new Error("当前更新源版本更新或无效，禁止回退");
      }
    } else if (!missing(stat)) {
      throw new Error("无法确认当前更新清单，停止发布");
    }
  }
  for (const file of plan.artifacts) {
    if (!existing.has(file.object)) {
      const result = await run([
        "--custom-header", "If-None-Match: *", "cp", "--disable-multipart", "--quiet", "--json",
        "--attr", "Cache-Control=public,max-age=31536000,immutable", file.path, file.object,
      ]);
      if (result.status !== 0) throw new Error(`条件上传 ${file.name} 失败，请核实源站后重试`);
    }
    if (await remoteHash(file.url) !== file.hash) {
      throw new Error(`CDN 上的 ${file.name} 校验不一致，未发布更新清单`);
    }
  }
  for (const feed of plan.feeds) {
    const result = await run([
      "cp", "--disable-multipart", "--quiet", "--json",
      "--attr", "Cache-Control=no-cache,max-age=0,must-revalidate", feed.path, feed.object,
    ]);
    if (result.status !== 0) throw new Error(`发布 ${feed.name} 失败`);
    if (await remoteHash(feed.url) !== feed.hash) {
      throw new Error(`${feed.name} 的 CDN 回读不一致，请检查 CDN 缓存规则后重试`);
    }
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2).filter((value) => value !== "--");
    const directoryIndex = args.indexOf("--dir");
    if (args.some((value, index) => !["--dry-run", "--dir", "--build"].includes(value) &&
          !(directoryIndex >= 0 && index === directoryIndex + 1)) ||
        (directoryIndex >= 0 && !args[directoryIndex + 1])) {
      throw new Error("用法：bun run publish [--dry-run] [--dir build]；bun run release [--dry-run]");
    }
    const root = fileURLToPath(new URL("../", import.meta.url));
    let directory = directoryIndex < 0 ? "build" : args[directoryIndex + 1];
    if (args.includes("--build")) {
      if (directoryIndex >= 0) throw new Error("--build 与 --dir 不能同时使用");
      if (!process.env.MYGO_UPDATER_PRIVATE_KEY?.trim()) throw new Error("请先设置 MYGO_UPDATER_PRIVATE_KEY");
      await mkdir(resolve(root, "build"), { recursive: true });
      directory = await mkdtemp(resolve(root, "build/release-"));
      const build = spawnSync("bun", ["run", "build", "--", "-platform", "darwin/arm64,darwin/amd64", "-o", directory], {
        cwd: root, stdio: "inherit",
        env: { ...process.env, GOMAXPROCS: "1", GOFLAGS: `${process.env.GOFLAGS ?? ""} -p=1`.trim() },
      });
      if (build.error || build.status !== 0) throw new Error("发布构建失败，未上传");
      console.log(`发布产物：${directory}`);
    }
    const plan = await releasePlan(root, directory);
    if (args.includes("--dry-run")) {
      console.log(`已校验 ${plan.version} 的双架构签名与安装包；将先上传并回读以下对象，再发布更新清单：`);
      console.log([...plan.artifacts, ...plan.feeds].map((file) => file.object).join("\n"));
    } else {
      await publish(plan);
      console.log(`已发布 XSourceTree ${plan.version}，双架构 CDN 文件及更新清单回读通过`);
    }
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
