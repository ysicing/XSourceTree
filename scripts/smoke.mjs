import assert from "node:assert/strict";
import net from "node:net";
import { execFileSync } from "node:child_process";
import { mkdtempSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const socketPath = process.env.MYGO_AUTOMATION;
assert(socketPath, "请设置 MYGO_AUTOMATION，指向启用 e2e 的应用 socket");
const fixture = mkdtempSync(join(tmpdir(), "xsourcetree-smoke-"));
const git = (...args) => execFileSync("git", ["-C", fixture, ...args], {
  encoding: "utf8",
  env: { ...process.env, GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1" },
}).trim();
git("init", "-b", "main");
git("config", "user.name", "XSourceTree Smoke");
git("config", "user.email", "smoke@example.test");
git("config", "commit.gpgsign", "false");
writeFileSync(join(fixture, "hello.txt"), "original\n");
git("add", ".");
git("commit", "-m", "initial fixture");
writeFileSync(join(fixture, "hello.txt"), 'changed\n<img src=x onerror="window.__xss=true">\n');
writeFileSync(join(fixture, "new file.txt"), "new file\n");

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
    const request = pending.get(reply.id);
    if (!request) continue;
    pending.delete(reply.id);
    clearTimeout(request.timeout);
    if (reply.error) request.reject(new Error(JSON.stringify(reply.error)));
    else request.resolve(reply.result);
  }
});
socket.on("error", (error) => {
  for (const request of pending.values()) {
    clearTimeout(request.timeout);
    request.reject(error);
  }
  pending.clear();
});

function request(method, params = {}) {
  const id = ++serial;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`automation timeout: ${method}`));
    }, 15000);
    pending.set(id, { resolve, reject, timeout });
    socket.write(JSON.stringify({ id, method, params }) + "\n");
  });
}

async function evaluate(script) {
  const result = await request("eval", { script });
  return result.value;
}

async function execute(script) {
  await evaluate(`(() => { ${script}\n})()`);
}

async function waitFor(script, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (await evaluate(script)) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  const error = await evaluate("document.querySelector('#alerts')?.textContent");
  throw new Error(`${label}：等待超时；界面错误：${error}`);
}

try {
  await execute("window.__smokeReloading = true; setTimeout(() => location.reload(), 0);");
  await waitFor("!window.__smokeReloading && !!document.querySelector('[data-action=\"open-path\"]')", "欢迎页");
  await execute(`window.__smokeErrors = [];
    window.addEventListener("error", event => {
      window.__smokeErrors.push({ message: event.message, source: event.filename, line: event.lineno, stack: event.error?.stack });
    });
    window.addEventListener("unhandledrejection", event => {
      window.__smokeErrors.push({ message: String(event.reason), stack: event.reason?.stack });
    });`);
  await evaluate("document.querySelector('[data-action=\"open-path\"]').click()");
  await execute(`document.querySelector('#dialog-input').value = ${JSON.stringify(fixture)}; document.querySelector('#input-form').requestSubmit();`);
  await waitFor("document.querySelectorAll('.file-row').length === 2 && !document.querySelector('#statusbar .spinner')", "打开仓库");
  await waitFor("document.querySelector('.diff-code')?.textContent.includes('+changed')", "真实文件 Diff");
  assert.equal(await evaluate("document.querySelectorAll('.diff-code img').length"), 0, "Diff 内容必须转义");
  assert.equal(await evaluate("window.__xss === undefined"), true);

  await execute("document.querySelector('#commit-message').value='桌面端集成测试'; document.querySelector('#commit-message').dispatchEvent(new Event('input', {bubbles:true}));");
  await evaluate("document.querySelector('[data-action=\"stage\"]').click()");
  await waitFor("document.querySelectorAll('[data-action=\"unstage\"]').length === 1 && !document.querySelector('#statusbar .spinner')", "暂存");
  assert.equal(await evaluate("document.querySelector('#commit-message').value"), "桌面端集成测试", "刷新不能丢失提交草稿");
  await evaluate("document.querySelector('[data-action=\"unstage\"]').click()");
  await waitFor("document.querySelectorAll('[data-action=\"unstage\"]').length === 0 && !document.querySelector('#statusbar .spinner')", "取消暂存");
  assert.equal(git("diff", "--cached", "--name-only"), "");

  await evaluate("document.querySelector('[data-action=\"stage-all\"]').click()");
  await waitFor("document.querySelectorAll('[data-action=\"unstage\"]').length === 2 && !document.querySelector('#statusbar .spinner')", "全部暂存");
  assert.equal(git("diff", "--cached", "--name-only").split("\n").length, 2);
  await evaluate("document.querySelector('#commit-form').requestSubmit()");
  await waitFor("!!document.querySelector('.clean-state') && !document.querySelector('#statusbar .spinner')", "提交");
  assert.equal(git("log", "-1", "--format=%s"), "桌面端集成测试");
  assert.equal(git("status", "--porcelain"), "");

  await evaluate("document.querySelector('[data-action=\"history\"]').click()");
  await waitFor("document.querySelectorAll('.commit-row').length === 2 && document.querySelector('.diff-code')?.textContent.includes('桌面端集成测试')", "提交历史与详情");
  assert.equal(await evaluate("document.querySelectorAll('.commit-graph').length"), 2);
  await evaluate("document.querySelector('[data-action=\"new-branch\"]').click()");
  await execute("document.querySelector('#dialog-input').value='feature/smoke'; document.querySelector('#input-form').requestSubmit()");
  await waitFor("document.querySelector('.branch-pill')?.textContent.includes('feature/smoke') && !document.querySelector('#statusbar .spinner')", "创建分支");
  assert.equal(git("branch", "--show-current"), "feature/smoke");
  await evaluate("document.querySelector('[data-action=\"switch\"][data-name=\"main\"]').click()");
  await waitFor("document.querySelector('.branch-pill')?.textContent.trim() === 'main' && !document.querySelector('#statusbar .spinner')", "切换分支");
  assert.equal(git("branch", "--show-current"), "main");

  const hook = join(fixture, ".git", "hooks", "post-checkout");
  writeFileSync(hook, "#!/bin/sh\necho smoke-checkout-failed >&2\nexit 1\n", { mode: 0o755 });
  await evaluate("document.querySelector('[data-action=\"switch\"][data-name=\"feature/smoke\"]').click()");
  await waitFor("document.querySelector('.branch-pill')?.textContent.trim() === 'feature/smoke' && document.querySelector('#alerts')?.textContent.includes('smoke-checkout-failed') && !document.querySelector('#statusbar .spinner')", "hook 失败后同步分支");
  assert.equal(git("branch", "--show-current"), "feature/smoke");
  rmSync(hook);

  writeFileSync(join(fixture, "hello.txt"), "external edit\n");
  await execute("document.querySelector('[data-action=\"changes\"]').click(); document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("document.querySelector('.diff-code')?.textContent.includes('+external edit') && !document.querySelector('#statusbar .spinner')", "外部修改刷新");

  const gitDir = join(fixture, ".git");
  const hiddenGitDir = join(fixture, ".git-smoke-hidden");
  renameSync(gitDir, hiddenGitDir);
  try {
    await evaluate("window.dispatchEvent(new Event('focus'))");
    await waitFor("document.querySelector('#alerts')?.textContent.includes('not a git repository') && [...document.querySelectorAll('[data-mutates]')].every(button => button.disabled)", "状态失效后禁用写操作");
    assert.equal(await evaluate("document.querySelector('[data-action=\"refresh\"]').disabled"), false, "失效后仍需允许刷新");
  } finally {
    renameSync(hiddenGitDir, gitDir);
  }
  await evaluate("document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("!document.querySelector('[data-action=\"stage-all\"]').disabled && !document.querySelector('#statusbar .spinner')", "刷新恢复写操作");
  assert.deepEqual(await evaluate("window.__smokeErrors"), [], "桌面页面不应出现未处理的脚本错误");

  if (process.env.XSOURCETREE_SCREENSHOT) {
    const capture = await request("capture");
    writeFileSync(process.env.XSOURCETREE_SCREENSHOT, Buffer.from(capture.png, "base64"));
  }
  console.log("PASS：真实 MyGo 桌面打开、Diff 转义、暂存/取消、草稿、提交、历史、分支、hook 失败同步、失效保护与刷新恢复");
  console.log(`测试仓库：${fixture}`);
} finally {
  socket.end();
}
