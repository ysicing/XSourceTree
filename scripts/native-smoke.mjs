/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import assert from "node:assert/strict";
import net from "node:net";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";

const socketPath = process.env.MYGO_AUTOMATION;
assert(socketPath, "请设置 MYGO_AUTOMATION，指向 e2e 应用 socket");
const root = mkdtempSync(join(tmpdir(), "xsourcetree-native-smoke-"));
const fixture = join(root, "中文仓库-" + basename(root).slice(-6));
const remote = join(root, "remote.git");
mkdirSync(fixture);
const gitEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_")));
Object.assign(gitEnv, { GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1", GIT_TERMINAL_PROMPT: "0" });
const gitAt = (path, ...args) => execFileSync("git", ["-C", path, ...args], {
  encoding: "utf8", env: gitEnv, timeout: 15000, stdio: ["ignore", "pipe", "pipe"],
}).trim();
const git = (...args) => gitAt(fixture, ...args);
git("init", "-b", "main");
git("config", "user.name", "Native Smoke");
git("config", "user.email", "native@example.test");
git("config", "commit.gpgsign", "false");
writeFileSync(join(fixture, "hello.txt"), "原始内容\n");
git("add", ".");
git("commit", "-m", "初始提交");
writeFileSync(join(fixture, "hello.txt"), "原生界面变更\n");
gitAt(root, "init", "--bare", remote);

const socket = net.createConnection(socketPath);
await new Promise((resolve, reject) => {
  socket.once("connect", resolve);
  socket.once("error", reject);
});
let serial = 0;
let buffer = "";
const pending = new Map();
socket.on("data", (data) => {
  buffer += data.toString();
  let boundary;
  while ((boundary = buffer.indexOf("\n")) >= 0) {
    const reply = JSON.parse(buffer.slice(0, boundary));
    buffer = buffer.slice(boundary + 1);
    const entry = pending.get(reply.id);
    if (!entry) continue;
    pending.delete(reply.id);
    clearTimeout(entry.timeout);
    if (reply.error) entry.reject(new Error(JSON.stringify(reply.error)));
    else entry.resolve(reply.result);
  }
});
socket.on("error", (error) => {
  for (const entry of pending.values()) {
    clearTimeout(entry.timeout);
    entry.reject(error);
  }
  pending.clear();
});
function request(method, params = {}) {
  return new Promise((resolve, reject) => {
    const id = ++serial;
    const timeout = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`automation timeout: ${method}`));
    }, 15000);
    pending.set(id, { resolve, reject, timeout });
    socket.write(JSON.stringify({ id, method, params }) + "\n");
  });
}
const tree = async () => {
  // 被其他窗口遮挡时，Native UI 可能保留待绘制帧；capture 使快照跟上当前状态。
  await request("capture");
  return (await request("tree")).nodes;
};
const delay = () => new Promise((resolve) => setTimeout(resolve, 100));
async function frame() {
  await request("idle", { timeout: 500 }).catch((error) => {
    if (!error.message.includes('"code":"timeout"')) throw error;
  });
}
async function waitFor(predicate, label) {
  const deadline = Date.now() + 20000;
  let nodes;
  while (Date.now() < deadline) {
    nodes = await tree();
    if (predicate(nodes)) return nodes;
    await delay();
  }
  throw new Error(`${label}超时\n${nodes.map((node) => node.label || node.placeholder || node.value).filter(Boolean).join("\n")}`);
}
async function ready() {
  await frame();
  // 弹窗会隐藏底部状态文字；其关闭按钮在后台任务执行期间同样被禁用。
  await waitFor((nodes) => !nodes.some((node) =>
    (["正在搜索历史…", "正在加载历史…"].includes(node.label)) ||
    (node.role === "text" && node.label?.startsWith("正在") && !node.label.startsWith("正在进行")) ||
    (node.role === "button" && ["取消", "关闭设置"].includes(node.label) && node.states?.includes("disabled"))), "后台任务");
}
async function act(label, action, text) {
  const nodes = await waitFor((entries) => entries.some((entry) =>
    (entry.label === label || entry.placeholder === label) && entry.actions?.includes(action)), label);
  const node = nodes.find((entry) =>
    (entry.label === label || entry.placeholder === label) && entry.actions?.includes(action));
  assert(node, `找不到可操作元素 ${label} / ${action}`);
  assert(!node.states?.includes("disabled"), `${label} 被禁用`);
  await request("act", { id: node.id, action, ...(text === undefined ? {} : { text }) });
  await frame();
}
async function press(label) {
  await act(label, "press");
  await ready();
}
const fill = (label, text) => act(label, "setValue", text);
const shown = (label) => waitFor((nodes) => nodes.some((node) => node.label === label), label);
async function capture(name) {
  if (!process.env.XSOURCETREE_TEST_SCREENSHOTS) return;
  const directory = process.env.XSOURCETREE_TEST_SCREENSHOTS;
  mkdirSync(directory, { recursive: true });
  const result = await request("capture");
  writeFileSync(join(directory, name), Buffer.from(result.png, "base64"));
}

try {
  const windows = (await request("windows")).windows;
  assert.equal(windows[0].kind, "content", "应用仍在使用 WebView");
  await request("menu", { path: ["文件", "本地仓库"] });
  await ready();
  await press("输入路径");
  await fill("仓库完整路径", fixture);
  await press("添加");
  await press(basename(fixture));
  await shown("文件变更");
  await press("搜索");
  await press("设置");
  const optionalSidebarItems = ["搜索", "版本比较", "贮藏", "工作树"];
  for (const label of optionalSidebarItems) await press(label);
  await capture("native-sidebar-settings-hidden.png");
  await press("关闭设置");
  let sidebarNodes = await tree();
  for (const label of optionalSidebarItems) {
    assert(!sidebarNodes.some((node) => node.label === label), `侧栏未隐藏 ${label}`);
  }
  await shown("未暂存文件");
  await capture("native-sidebar-minimal.png");
  await press("设置");
  await press("贮藏");
  await press("关闭设置");
  sidebarNodes = await tree();
  assert(sidebarNodes.some((node) => node.label === "贮藏"));
  for (const label of ["搜索", "版本比较", "工作树"]) {
    assert(!sidebarNodes.some((node) => node.label === label), `贮藏开关影响了 ${label}`);
  }
  await press("设置");
  for (const label of ["搜索", "版本比较", "工作树"]) await press(label);
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press(appearance);
    await capture(`native-sidebar-settings-${suffix}.png`);
  }
  await press("关闭设置");
  console.log("✓ 侧栏独立开关、当前页面回退、重新开启及浅深色设置");
  await press("提交历史");
  await press("未提交更改 · 1 个文件");
  await shown("未暂存文件");
  await capture("native-desktop-working-history.png");
  await press("前往文件变更");
  await press("hello.txt");
  await press("差异");
  await press("差异布局");
  await press("并排");
  await capture("native-desktop-diff.png");
  await press("全文");
  const full = (await shown("文件全文")).find((node) => node.label === "文件全文");
  assert(full.states?.includes("readOnly"), "全文预览必须只读");
  await capture("native-desktop-full-file.png");
  await press("差异");
  await press("暂存文件");
  await fill("提交说明", "使用原生 UI 提交");
  await request("menu", { path: ["仓库", "提交"] });
  await shown("提交 0 个文件");
  await ready();
  assert.equal(git("log", "-1", "--format=%s"), "使用原生 UI 提交");
  assert.equal(git("status", "--porcelain"), "");
  console.log("✓ 原生窗口、中文输入、并排 Diff、暂存和菜单提交");

  await press("版本比较");
  await fill("基准版本", "HEAD~1");
  await fill("目标版本", "HEAD");
  await press("比较");
  await shown("hello.txt");
  await shown("比较结果");
  await capture("native-desktop-compare.png");
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press("设置");
    await press(appearance);
    await press("关闭设置");
    await capture(`native-desktop-compare-${suffix}.png`);
  }
  await press("全文");
  const comparedFull = (await shown("文件全文")).find((node) => node.label === "文件全文");
  assert(comparedFull.states?.includes("readOnly"), "比较全文必须只读");
  await capture("native-desktop-compare-full.png");
  await press("差异");
  await press("交换比较方向");
  await shown("比较结果");
  await capture("native-desktop-compare-reverse.png");
  await press("共同祖先");
  await press("比较");
  await shown("两个版本之间没有文件变更");
  await capture("native-desktop-compare-empty.png");
  await press("文件变更");
  console.log("✓ 两版本比较、只读全文、交换方向和共同祖先空状态");

  await press("分支");
  await fill("分支名称", "main");
  await shown("本地分支 main 已存在");
  await capture("native-reference-name-conflict.png");
  await fill("分支名称", "native-topic");
  await shown("本地名称可用");
  await capture("native-reference-create-branch.png");
  await press("创建并切换");
  assert.equal(git("branch", "--show-current"), "native-topic");
  await press("main");
  assert.equal(git("branch", "--show-current"), "main");
  await press("提交历史");
  git("tag", "native-smoke");
  await request("menu", { path: ["仓库", "刷新"] });
  await ready();
  assert.equal(git("tag", "--list"), "native-smoke");
  await press("搜索");
  await fill("搜索提交、作者或 ID", "使用原生 UI");
  await press("执行搜索");
  await waitFor((nodes) => nodes.some((node) => node.label?.includes("使用原生 UI 提交")), "历史结果");
  await press("使用原生 UI 提交");
  await shown("在提交历史中查看");
  await capture("native-desktop-search.png");
  await press("在提交历史中查看");
  await shown("提交历史表格");
  console.log("✓ 分支创建与切换、标签加载、历史搜索");

  const tagHead = git("rev-parse", "HEAD");
  const tagTarget = git("rev-parse", "HEAD^");
  git("tag", "-a", "--no-sign", "-m", "旧版本", "native-annotated", tagTarget);
  await request("menu", { path: ["仓库", "刷新"] });
  await ready();
  await press("标签");
  await press("native-annotated");
  await shown("初始提交");
  await shown("完整提交 ID");
  await waitFor((nodes) => nodes.some((node) =>
    node.role === "row" && node.key === tagTarget && node.states?.includes("checked")), "标签提交定位");
  assert.equal(git("rev-parse", "HEAD"), tagHead);
  assert.equal(git("branch", "--show-current"), "main");
  await capture("native-desktop-tag-selected.png");
  console.log("✓ 搜索返回历史、附注标签定位、保持当前分支");

  await press("文件变更");
  await press("仓库设置");
  await fill("姓名", "Native Settings");
  await press("保存");
  assert.equal(git("config", "--local", "user.name"), "Native Settings");
  await capture("native-desktop-repository-settings.png");
  await press("远程");
  await press("添加远程");
  await fill("名称", "origin");
  await fill("获取地址", remote);
  await press("保存");
  assert.equal(git("remote", "get-url", "origin"), remote);
  await press("关闭设置");
  await press("推送");
  await shown("发布当前分支");
  await press("发布");
  assert.equal(git("rev-parse", "--abbrev-ref", "@{upstream}"), "origin/main");
  assert.equal(gitAt(remote, "rev-parse", "refs/heads/main"), git("rev-parse", "HEAD"));
  console.log("✓ 远程配置、首次发布和上游跟踪（临时本地 bare 仓库）");

  const backup = join(root, "backup.git");
  gitAt(root, "init", "--bare", backup);
  git("remote", "add", "backup", backup);
  const originalRemoteHead = gitAt(remote, "rev-parse", "main");
  git("commit", "--allow-empty", "-m", "选择推送远程");
  git("branch", "aaa");
  git("branch", "a/topic");
  git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main");
  await press("刷新");
  const branchNodes = await tree();
  const mainBranch = branchNodes.find((node) => node.role === "treeItem" && node.label === "main");
  const directory = branchNodes.find((node) => node.label === "本地分支目录 a");
  assert(mainBranch && directory && mainBranch.bounds.y < directory.bounds.y, "默认分支未排在目录前");
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press("设置");
    await press(appearance);
    await press("关闭设置");
    await press("推送");
    await shown("远程分支名称");
    await capture(`native-push-remote-selection-${suffix}.png`);
    if (suffix === "light") await press("取消");
  }
  await press("origin");
  await press("backup");
  await shown(backup);
  await press("推送");
  assert.equal(gitAt(backup, "rev-parse", "refs/heads/main"), git("rev-parse", "HEAD"));
  assert.equal(gitAt(remote, "rev-parse", "refs/heads/main"), originalRemoteHead);
  assert.equal(git("rev-parse", "--abbrev-ref", "@{upstream}"), "origin/main");
  console.log("✓ 默认分支置顶、多远程选择推送并保留已有上游");

  const scrollDirectory = join(fixture, "scroll-fixture");
  mkdirSync(scrollDirectory);
  const scrollPath = (index) => `scroll-fixture/${"long-path-".repeat(8)}file-${String(index).padStart(3, "0")}.go`;
  for (let index = 0; index < 100; index++) writeFileSync(join(fixture, scrollPath(index)), "scroll\n");
  await press("刷新");
  const scrollNodes = await tree();
  const fileList = scrollNodes.find((node) => node.label === "未暂存文件列表");
  const firstFile = scrollNodes.find((node) => node.label === scrollPath(0));
  assert(fileList && firstFile);
  const wheel = async (dx, dy) => {
    await request("input", { events: [{
      type: "wheel", precise: true, dx, dy,
      x: fileList.bounds.x + fileList.bounds.width / 2,
      y: fileList.bounds.y + fileList.bounds.height / 2,
    }] });
    await frame();
  };
  await wheel(0, 400);
  await wheel(0, -1000);
  for (let index = 0; index < 4; index++) await wheel(6, -50);
  let scrolledFile = (await tree()).find((node) => node.label === scrollPath(0));
  assert.equal(scrolledFile?.bounds.x, firstFile.bounds.x, "顶部纵向滑动导致路径横移");
  await wheel(80, 0);
  scrolledFile = (await tree()).find((node) => node.label === scrollPath(0));
  assert(scrolledFile?.bounds.x < firstFile.bounds.x, "主动横向滑动失效");
  await capture("native-file-list-scroll.png");
  rmSync(scrollDirectory, { recursive: true });
  await press("刷新");
  console.log("✓ 文件列表顶部纵向滚动保持位置、主动横向滚动");

  writeFileSync(join(fixture, "hello.txt"), "贮藏修改\n");
  await press("刷新");
  await press("贮藏");
  await press("贮藏更改…");
  await fill("说明（可选）", "原生贮藏");
  await press("贮藏");
  assert.equal(git("status", "--porcelain"), "");
  await press("贮藏");
  const stashNodes = await waitFor((nodes) => nodes.some((node) =>
    node.role === "button" && node.label?.includes("原生贮藏")), "贮藏记录");
  const stashNode = stashNodes.find((node) => node.role === "button" && node.label?.includes("原生贮藏"));
  assert(stashNode, "未显示贮藏记录");
  await press(stashNode.label);
  await capture("native-stash.png");
  await press("应用并移除");
  await shown("应用并移除贮藏？");
  await press("应用并移除");
  assert.equal(readFileSync(join(fixture, "hello.txt"), "utf8"), "贮藏修改\n");
  assert.equal(git("stash", "list"), "");
  await press("文件变更");
  await press("hello.txt");
  await press("丢弃修改…");
  await press("丢弃修改");
  assert.equal(git("status", "--porcelain"), "");
  console.log("✓ 创建、预览、应用贮藏，以及备份后丢弃修改");

  const linked = join(root, "中文工作树");
  const mainHead = git("rev-parse", "HEAD");
  await press("工作树");
  await press("新建工作树…");
  await fill("工作目录", linked);
  await fill("分支名称", "native-worktree");
  await fill("起点", "HEAD");
  await capture("native-worktree-create.png");
  await press("创建");
  await shown(realpathSync(linked));
  assert.equal(git("rev-parse", "HEAD"), mainHead);
  assert.equal(gitAt(linked, "branch", "--show-current"), "native-worktree");
  await press(realpathSync(linked));
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press("设置");
    await press(appearance);
    await press("关闭设置");
    await capture(`native-worktrees-${suffix}.png`);
  }
  await press("打开工作树");
  await shown("文件变更");
  writeFileSync(join(linked, "worktree-feature.txt"), "来自链接工作树\n");
  gitAt(linked, "add", ".");
  gitAt(linked, "commit", "-m", "worktree feature");
  await press("工作树");
  await press(realpathSync(fixture));
  await press("打开工作树");
  await press("工作树");
  await press(realpathSync(linked));
  assert(existsSync(linked), "打开另一个工作目录不应移除链接工作树");
  assert.equal(git("rev-parse", "HEAD"), mainHead, "切换工作树不应隐式合并提交");
  console.log("✓ 工作树创建、发现、打开与浅深色界面");

  await request("menu", { path: ["文件", "本地仓库"] });
  await ready();
  await shown("输入路径");
  await capture("native-desktop-library.png");
  await press("新建仓库组");
  await fill("组名", "日常开发");
  await press("保存");
  await shown("日常开发");
  console.log("✓ 原生菜单、仓库列表和分组");

  const paged = join(root, "连续滚动历史");
  mkdirSync(paged);
  gitAt(paged, "init", "-b", "main");
  let historyStream = "";
  for (let index = 0; index < 625; index++) {
    const subject = `滚动提交 ${String(index).padStart(5, "0")}`;
    historyStream += `commit refs/heads/main\nmark :${index + 1}\ncommitter Native Smoke <native@example.test> ${1800000000 + index} +0000\ndata ${Buffer.byteLength(subject)}\n${subject}\n`;
    if (index > 0) historyStream += `from :${index}\n`;
    const content = `history version ${index}\n`;
    historyStream += `M 100644 inline history.txt\ndata ${Buffer.byteLength(content)}\n${content}\n`;
  }
  execFileSync("git", ["-C", paged, "fast-import", "--quiet"], {
    input: historyStream, env: gitEnv, timeout: 15000, stdio: ["pipe", "pipe", "pipe"],
  });
  gitAt(paged, "restore", "--source=HEAD", "--staged", "--worktree", ".");
  const longHistoryBranch = "origin/dependabot/go_modules/golang.org/x/sys-0.50.0";
  gitAt(paged, "branch", "feature/完整显示所有分支名称");
  gitAt(paged, "update-ref", "refs/remotes/origin/main", "HEAD");
  gitAt(paged, "update-ref", `refs/remotes/${longHistoryBranch}`, "HEAD");
  gitAt(paged, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main");
  gitAt(paged, "tag", "v1.0.0-preview");
  await press("输入路径");
  await fill("仓库完整路径", paged);
  await press("添加");
  await press(basename(paged));
  await press("提交历史");
  await shown("滚动提交 00624");
  await press("滚动提交 00624");
  await press("history.txt");
  assert(!(await tree()).some((node) => node.label === "加载更多"), "历史仍显示加载更多按钮");
  await capture("native-history-initial.png");
  const fullHistoryRefs = (await tree()).find((node) =>
    node.role === "text" && node.label?.includes(longHistoryBranch) && !node.label.includes(", "));
  assert(fullHistoryRefs, "历史列表未显示完整长分支名");
  for (const ref of ["HEAD -> main", "origin/main", "origin/HEAD", "feature/完整显示所有分支名称", "v1.0.0-preview"]) {
    assert(fullHistoryRefs.label.includes(ref), `历史列表遗漏引用 ${ref}`);
  }
  const refsTable = (await tree()).find((node) => node.label === "提交历史表格");
  const scrollHistoryRefs = async (dx) => {
    await request("input", { events: [{
      type: "wheel", precise: true, dx, dy: 0,
      x: refsTable.bounds.x + refsTable.bounds.width / 2,
      y: fullHistoryRefs.bounds.y + fullHistoryRefs.bounds.height / 2,
    }] });
    await frame();
  };
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press("设置");
    await press(appearance);
    await press("关闭设置");
    await capture(`native-history-full-refs-${suffix}.png`);
    await scrollHistoryRefs(1e6);
    const scrolledRefs = (await tree()).find((node) => node.label === fullHistoryRefs.label);
    assert(scrolledRefs?.bounds.x < fullHistoryRefs.bounds.x, "完整引用无法通过横向滚动查看");
    const message = (await tree()).find((node) => node.role === "text" && node.label === "滚动提交 00624");
    assert(message && message.bounds.x >= refsTable.bounds.x &&
      message.bounds.x + message.bounds.width <= refsTable.bounds.x + refsTable.bounds.width,
    "横向滚动后无法查看提交说明");
    await capture(`native-history-full-refs-scrolled-${suffix}.png`);
    await scrollHistoryRefs(-1e6);
  }
  console.log("✓ 完整分支和标签名称、横向滚动与浅深色界面");
  for (let attempt = 0; attempt < 12; attempt++) {
    const historyNodes = await tree();
    if (historyNodes.some((node) => node.label?.includes("滚动提交 00000"))) break;
    const historyTable = historyNodes.find((node) => node.label === "提交历史表格");
    assert(historyTable, "续载时历史表格消失");
    await request("input", { events: [{
      type: "wheel", precise: true, dx: 0, dy: 2200,
      x: historyTable.bounds.x + historyTable.bounds.width / 2,
      y: historyTable.bounds.y + historyTable.bounds.height / 2,
    }] });
    await ready();
  }
  await waitFor((nodes) => nodes.some((node) => node.label?.includes("滚动提交 00000")), "滚动续载到最早提交");
  assert(!(await tree()).some((node) => node.label === "加载更多"), "续载后出现加载更多按钮");
  await shown("history.txt");
  for (const [appearance, suffix] of [["浅色", "light"], ["深色", "dark"]]) {
    await press("设置");
    await press(appearance);
    await press("关闭设置");
    await capture(`native-history-pagination-${suffix}.png`);
  }
  console.log("✓ 历史连续滚动到最早提交、无加载更多按钮与浅深色界面");
  console.log("Native UI 桌面 smoke 通过");
} finally {
  socket.end();
  rmSync(root, { recursive: true, force: true });
}
