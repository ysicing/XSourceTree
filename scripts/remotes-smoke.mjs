import assert from "node:assert/strict";
import net from "node:net";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const socketPath = process.env.MYGO_AUTOMATION;
assert(socketPath, "请设置 MYGO_AUTOMATION，指向启用 e2e 的应用 socket");
const fixture = mkdtempSync(join(tmpdir(), "xsourcetree-remotes-"));
const local = join(fixture, "local");
const remote = join(fixture, "remote.git");
const alternative = join(fixture, "alternative.git");
const git = (directory, ...args) => execFileSync("git", ["-C", directory, ...args], {
  encoding: "utf8",
  env: { ...process.env, GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1" },
}).trim();
git(fixture, "init", "-b", "main", local);
git(fixture, "init", "--bare", "-b", "main", remote);
git(fixture, "init", "--bare", "-b", "main", alternative);
git(local, "config", "user.name", "Remote Smoke");
git(local, "config", "user.email", "smoke@example.test");
git(local, "config", "commit.gpgsign", "false");
writeFileSync(join(local, "hello.txt"), "initial\n");
git(local, "add", ".");
git(local, "commit", "-m", "initial");

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
    const operation = pending.get(reply.id);
    if (!operation) continue;
    pending.delete(reply.id);
    clearTimeout(operation.timeout);
    if (reply.error) operation.reject(new Error(JSON.stringify(reply.error)));
    else operation.resolve(reply.result);
  }
});
socket.on("error", (error) => {
  for (const operation of pending.values()) {
    clearTimeout(operation.timeout);
    operation.reject(error);
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
  return (await request("eval", { script })).value;
}

async function execute(script) {
  await evaluate(`(() => { ${script}\n})()`);
}

async function waitFor(script, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    try {
      if (await evaluate(script)) return;
    } catch (error) {
      if (!error.message.includes('"no_window"')) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`${label}：等待超时；${await evaluate("document.querySelector('#alerts')?.textContent")}`);
}

async function click(selector) {
  assert(await evaluate(`!!document.querySelector(${JSON.stringify(selector)})`), `缺少 ${selector}`);
  await evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
}

async function submit(form, fields = {}) {
  await execute(`${Object.entries(fields).map(([selector, value]) => `document.querySelector(${JSON.stringify(selector)}).value=${JSON.stringify(value)};`).join("\n")}
    document.querySelector(${JSON.stringify(form)}).requestSubmit();`);
  await waitFor("!document.querySelector('#statusbar .spinner') && !document.querySelector('dialog[open]')", form);
}

async function refresh() {
  await click('[data-action="refresh"]');
  await waitFor("!document.querySelector('#statusbar .spinner')", "刷新");
}

async function capture(name) {
  if (!process.env.XSOURCETREE_SCREENSHOT_DIR) return;
  const result = await request("capture");
  writeFileSync(join(process.env.XSOURCETREE_SCREENSHOT_DIR, name), Buffer.from(result.png, "base64"));
}

try {
  await waitFor("!!document.querySelector('#repo-search')", "本地仓库页");
  await execute(`window.__remoteErrors = [];
    window.addEventListener("error", event => window.__remoteErrors.push(event.message));
    window.addEventListener("unhandledrejection", event => window.__remoteErrors.push(String(event.reason)));`);
  await click(".add-menu summary");
  await click('[data-action="open-path"]');
  await submit("#library-add-form", { "#dialog-input": local });
  await click(`[data-action="open"][data-path="${realpathSync(local)}"]`);
  await waitFor("!!document.querySelector('[data-action=\"publish-branch\"]') && !document.querySelector('#statusbar .spinner')", "打开仓库");

  await click('#toolbar [data-action="add-remote"]');
  await submit("#remote-form", { "#remote-url": alternative });
  assert.equal(git(local, "remote", "get-url", "origin"), alternative);
  await click('[data-action="remote-settings"]');
  await click('[data-action="edit-remote"][data-name="origin"]');
  await submit("#remote-form", { "#remote-url": remote });
  assert.equal(git(local, "remote", "get-url", "origin"), remote);
  await click('[data-action="remote-settings"]');
  await click('[data-action="add-remote"]');
  await submit("#remote-form", { "#remote-name": "backup", "#remote-url": alternative });
  await click('[data-action="remote-settings"]');
  await capture("remote-settings.png");
  await submit("#remote-settings-form");
  await click('[data-action="publish-branch"]');
  await execute("document.querySelector('#publish-remote').value='backup'; document.querySelector('#publish-remote').dispatchEvent(new Event('change', {bubbles:true}));");
  assert.equal(await evaluate("document.querySelector('#publish-destination').textContent"), alternative);
  await click('[data-action="cancel-dialog"]');
  await click('[data-action="remote-settings"]');
  await click('[data-action="remove-remote"][data-name="backup"]');
  await click('[data-action="cancel-dialog"]');
  assert.equal(git(local, "remote", "get-url", "backup"), alternative);
  await click('[data-action="remote-settings"]');
  await click('[data-action="remove-remote"][data-name="backup"]');
  await submit("#confirm-form");
  assert.equal(git(local, "remote"), "origin");

  await click('[data-action="publish-branch"]');
  git(local, "remote", "set-url", "origin", alternative);
  await refresh();
  assert.equal(await evaluate("document.querySelector('#publish-destination').textContent"), remote);
  await submit("#publish-form");
  assert.match(await evaluate("document.querySelector('#alerts').textContent"), /远程配置已改变/);
  assert.equal(git(alternative, "for-each-ref", "--format=%(refname)"), "");
  assert.equal(git(local, "for-each-ref", "--format=%(upstream)", "refs/heads/main"), "");
  git(local, "remote", "set-url", "origin", remote);
  await refresh();

  await click('[data-action="publish-branch"]');
  git(local, "commit", "--allow-empty", "-m", "external commit");
  await submit("#publish-form");
  assert.match(await evaluate("document.querySelector('#alerts').textContent"), /当前分支或提交已改变/);
  assert.equal(git(remote, "for-each-ref", "--format=%(refname)"), "");
  await refresh();
  await click('[data-action="publish-branch"]');
  await execute("document.querySelector('#publish-name').value='release/demo';");
  await capture("remote-publish.png");
  await submit("#publish-form");
  assert.equal(git(local, "rev-parse", "--abbrev-ref", "@{upstream}"), "origin/release/demo");
  assert.equal(git(remote, "rev-parse", "refs/heads/release/demo"), git(local, "rev-parse", "HEAD"));
  assert(await evaluate("!!document.querySelector('[data-action=\"push\"]')"), "发布成功应变为普通推送");
  git(local, "commit", "--allow-empty", "-m", "next commit");
  await refresh();
  await click('[data-action="push"]');
  await waitFor("!document.querySelector('#statusbar .spinner')", "普通推送");
  assert.equal(git(remote, "rev-parse", "refs/heads/release/demo"), git(local, "rev-parse", "HEAD"));

  git(remote, "update-ref", "refs/heads/feature/review", git(local, "rev-parse", "HEAD"));
  await click('[data-action="fetch"]');
  await waitFor("!document.querySelector('#statusbar .spinner')", "获取分支");
  await click('.remote-group[data-remote="origin"] summary');
  await click('[data-action="checkout-remote"][data-name="origin/feature/review"]');
  assert.equal(await evaluate("document.querySelector('#checkout-name').value"), "feature/review");
  await capture("remote-checkout.png");
  await submit("#checkout-form", { "#checkout-name": "review" });
  assert.equal(git(local, "branch", "--show-current"), "review");
  assert.equal(git(local, "rev-parse", "--abbrev-ref", "@{upstream}"), "origin/feature/review");
  git(local, "switch", "main");
  writeFileSync(join(local, "draft.txt"), "keep my draft\n");
  await refresh();
  await click('[data-action="checkout-remote"][data-name="origin/feature/review"]');
  await waitFor("!document.querySelector('#statusbar .spinner') && document.querySelector('.branch-pill')?.textContent.includes('review')", "切换已有跟踪分支");
  assert.equal(git(local, "branch", "--show-current"), "review");
  assert.equal(readFileSync(join(local, "draft.txt"), "utf8"), "keep my draft\n");
  assert.equal(await evaluate("!!document.querySelector('dialog[open]')"), false);
  await capture("remote-workspace.png");
  assert.deepEqual(await evaluate("window.__remoteErrors"), []);
  console.log("PASS：原生远程添加/编辑/取消移除/移除、目标与提交变化保护、首次发布及上游、普通推送、获取与检出远程分支、切换已有跟踪分支并保留草稿");
  console.log(`测试仓库：${fixture}`);
} finally {
  socket.end();
}
