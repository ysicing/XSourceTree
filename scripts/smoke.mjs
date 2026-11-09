import assert from "node:assert/strict";
import net from "node:net";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const socketPath = process.env.MYGO_AUTOMATION;
assert(socketPath, "请设置 MYGO_AUTOMATION，指向启用 e2e 的应用 socket");
const fixture = mkdtempSync(join(tmpdir(), "xsourcetree-smoke-"));
const gitAt = (directory, ...args) => execFileSync("git", ["-C", directory, ...args], {
  encoding: "utf8",
  env: { ...process.env, GIT_CONFIG_GLOBAL: "/dev/null", GIT_CONFIG_NOSYSTEM: "1" },
}).trim();
const git = (...args) => gitAt(fixture, ...args);
const cloneParent = mkdtempSync(join(tmpdir(), "xsourcetree-clone-smoke-"));
const cloned = join(cloneParent, "cloned");
const scanRoot = mkdtempSync(join(tmpdir(), "xsourcetree-library-smoke-"));
const scanFirst = join(scanRoot, "应用", "GenWorld");
const scanSecond = join(scanRoot, "工具", "XSourceTree");
execFileSync("git", ["init", "-b", "main", scanFirst]);
execFileSync("git", ["init", "-b", "develop", scanSecond]);
execFileSync("git", ["init", "-b", "main", join(scanRoot, "node_modules", "ignored")]);
const scanPaths = [realpathSync(scanFirst), realpathSync(scanSecond)];
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

async function observeErrors() {
  await execute(`window.__smokeErrors = [];
    window.addEventListener("error", event => {
      window.__smokeErrors.push({ message: event.message, source: event.filename, line: event.lineno, stack: event.error?.stack });
    });
    window.addEventListener("unhandledrejection", event => {
      window.__smokeErrors.push({ message: String(event.reason), stack: event.reason?.stack });
    });`);
}

async function addLocal(path, group = "") {
  await evaluate("document.querySelector('.add-menu summary').click()");
  await evaluate("document.querySelector('[data-action=\"open-path\"]').click()");
  await execute(`document.querySelector('#dialog-input').value=${JSON.stringify(path)}; document.querySelector('#destination-group').value=${JSON.stringify(group)}; document.querySelector('#library-add-form').requestSubmit();`);
  await waitFor("!document.querySelector('#statusbar .spinner') && !document.querySelector('dialog[open]')", "添加本地仓库");
}

async function clickRepository(path, action = "open") {
  const canonical = realpathSync(path);
  if (action !== "open") {
    await evaluate(`[...document.querySelectorAll('.library-row')].find(row => row.dataset.repository === ${JSON.stringify(canonical)}).querySelector('summary').click()`);
  }
  await evaluate(`[...document.querySelectorAll('[data-action="${action}"]')].find(button => button.dataset.path === ${JSON.stringify(canonical)}).click()`);
}

try {
  await execute("window.__smokeReloading = true; setTimeout(() => location.reload(), 0);");
  await waitFor("!window.__smokeReloading && !!document.querySelector('#repo-search')", "本地仓库页");
  await observeErrors();
  assert.equal(await evaluate("document.querySelectorAll('[data-action=\"clone\"]').length"), 0);
  const groupName = `macOS-${fixture.split("-").pop()}`;
  await evaluate("document.querySelector('[data-action=\"new-group\"]').click()");
  await execute(`document.querySelector('#group-name').value=${JSON.stringify(groupName)}; document.querySelector('#group-form').requestSubmit();`);
  await waitFor(`document.querySelector('.library-heading h1')?.textContent.includes(${JSON.stringify(groupName)}) && !document.querySelector('#statusbar .spinner')`, "创建仓库组");
  const groupID = await evaluate("document.querySelector('.group-navigation .selected').dataset.group");
  await addLocal(fixture, groupID);
  await waitFor("document.querySelectorAll('.library-row').length === 1", "添加已有仓库");
  assert.equal(await evaluate("document.querySelectorAll('.file-row').length"), 0, "添加不应自动打开");
  await addLocal(fixture, "");
  assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 1, "重复添加不能创建副本或改变分组");
  for (const query of [fixture.split("/").pop().toUpperCase(), "main", groupName]) {
    await execute(`document.querySelector('#repo-search').value=${JSON.stringify(query)}; document.querySelector('#repo-search').dispatchEvent(new Event('input',{bubbles:true}));`);
    assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 1, `搜索 ${query}`);
  }
  await execute("document.querySelector('#repo-search').value='no-such-repository'; document.querySelector('#repo-search').dispatchEvent(new Event('input',{bubbles:true}));");
  assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 0);
  await evaluate("document.querySelector('[data-action=\"clear-repo-search\"]').click()");
  await evaluate("document.querySelector('.add-menu summary').click()");
  await evaluate("document.querySelector('[data-action=\"scan\"]').click()");
  await execute(`document.querySelector('#scan-root').value=${JSON.stringify(scanRoot)}; document.querySelector('#scan-form').requestSubmit();`);
  await waitFor("document.querySelectorAll('.scan-result').length === 2", "扫描嵌套本地仓库");
  assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 1, "扫描不能自动加入列表");
  await execute(`const checkboxes=[...document.querySelectorAll('input[name="repository"]')]; checkboxes.find(input=>input.value === ${JSON.stringify(scanPaths[1])}).click();`);
  assert.equal(await evaluate("document.querySelector('#scan-selected-count').textContent"), "已选 1 个");
  await evaluate("document.querySelector('#scan-import-form').requestSubmit()");
  await waitFor("document.querySelectorAll('.library-row').length === 2 && !document.querySelector('#statusbar .spinner')", "勾选后导入指定组");
  await evaluate("document.querySelector('.add-menu summary').click()");
  await evaluate("document.querySelector('[data-action=\"scan\"]').click()");
  await execute(`document.querySelector('#scan-root').value=${JSON.stringify(scanRoot)}; document.querySelector('#scan-form').requestSubmit();`);
  await waitFor("document.querySelectorAll('.scan-result').length === 2", "重新扫描");
  assert.equal(await evaluate("document.querySelectorAll('.scan-result input:disabled').length"), 1, "已经添加的仓库应禁选");
  await evaluate("document.querySelector('#scan-select-all').click()");
  assert.equal(await evaluate("document.querySelector('#scan-import-form button[type=\"submit\"]').disabled"), true, "未勾选不能导入");
  await evaluate("document.querySelector('#scan-select-all').click()");
  await evaluate("document.querySelector('#scan-import-form').requestSubmit()");
  await waitFor("document.querySelectorAll('.library-row').length === 3 && !document.querySelector('#statusbar .spinner')", "导入剩余仓库");

  await evaluate("document.querySelector('[data-action=\"rename-group\"]').click()");
  const renamedGroup = `工作-${fixture.split("-").pop()}`;
  await execute(`document.querySelector('#group-name').value=${JSON.stringify(renamedGroup)}; document.querySelector('#group-form').requestSubmit();`);
  await waitFor(`document.querySelector('.library-heading h1')?.textContent.includes(${JSON.stringify(renamedGroup)}) && !document.querySelector('#statusbar .spinner')`, "重命名仓库组");
  assert.deepEqual(await evaluate("window.__smokeErrors"), []);
  await execute("window.__smokeReloading=true; setTimeout(()=>location.reload(),0);");
  await waitFor(`!window.__smokeReloading && [...document.querySelectorAll('.group-navigation button')].some(button=>button.textContent.includes(${JSON.stringify(renamedGroup)})) && document.querySelectorAll('.library-row').length >= 3`, "重载保留仓库与组");
  await observeErrors();
  await execute(`document.querySelector('#repo-search').value=${JSON.stringify(renamedGroup)}; document.querySelector('#repo-search').dispatchEvent(new Event('input',{bubbles:true}));`);
  assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 3, "可按组名搜索");
  if (process.env.XSOURCETREE_LIBRARY_SCREENSHOT) {
    const capture = await request("capture");
    writeFileSync(process.env.XSOURCETREE_LIBRARY_SCREENSHOT, Buffer.from(capture.png, "base64"));
  }
  await clickRepository(scanFirst, "move-repository");
  await execute("document.querySelector('#destination-group').value=''; document.querySelector('#move-form').requestSubmit();");
  await waitFor("document.querySelectorAll('.library-row').length === 2 && !document.querySelector('#statusbar .spinner')", "移出仓库组");
  await execute("document.querySelector('#repo-search').value=''; document.querySelector('#repo-search').dispatchEvent(new Event('input',{bubbles:true}));");
  await evaluate(`[...document.querySelectorAll('[data-action="library-group"]')].find(button=>button.dataset.group === ${JSON.stringify(groupID)}).click()`);
  await evaluate("document.querySelector('[data-action=\"remove-group\"]').click()");
  assert.equal(await evaluate("document.querySelectorAll('.library-row').length"), 2, "确认前保留组");
  await evaluate("document.querySelector('#confirm-form').requestSubmit()");
  await waitFor("!document.querySelector('[data-action=\"remove-group\"]') && !document.querySelector('#statusbar .spinner')", "移除仓库组");
  assert.equal(existsSync(join(scanSecond, ".git")), true);
  await clickRepository(scanSecond, "remove-repository");
  await evaluate("document.querySelector('#confirm-form').requestSubmit()");
  await waitFor(`![...document.querySelectorAll('.library-row')].some(row=>row.dataset.repository === ${JSON.stringify(scanPaths[1])}) && !document.querySelector('#statusbar .spinner')`, "从列表移除仓库");
  assert.equal(existsSync(join(scanSecond, ".git")), true, "移除列表项不能删除仓库");
  await clickRepository(fixture);
  await waitFor("document.querySelectorAll('.file-row').length === 2 && !document.querySelector('#statusbar .spinner')", "打开仓库");
  await waitFor("document.querySelector('.diff-code')?.textContent.includes('+changed')", "真实文件 Diff");
  assert.equal(await evaluate("document.querySelectorAll('.diff-code img').length"), 0, "Diff 内容必须转义");
  assert.equal(await evaluate("window.__xss === undefined"), true);
  assert.equal(await evaluate("document.querySelector('.file-group').dataset.group"), "staged");
  assert.equal(await evaluate("document.querySelector('#commit-form').parentElement.id"), "content");
  await execute("document.querySelector('#filter').value='hello'; document.querySelector('#filter').dispatchEvent(new Event('input', {bubbles:true}));");
  await evaluate("document.querySelector('.file-check:not(:disabled)').click()");
  await evaluate("document.querySelector('[data-action=\"stage-all\"]').click()");
  await waitFor("document.querySelectorAll('[data-action=\"unstage\"]').length === 1 && !document.querySelector('#statusbar .spinner')", "筛选并批量暂存");
  assert.equal(git("diff", "--cached", "--name-only"), "hello.txt", "批量操作只能影响匹配文件");
  await evaluate("document.querySelector('[data-action=\"unstage-all\"]').click()");
  await waitFor("!document.querySelector('[data-action=\"unstage\"]') && !document.querySelector('#statusbar .spinner')", "批量取消暂存");
  await execute("document.querySelector('#filter').value=''; document.querySelector('#filter').dispatchEvent(new Event('input', {bubbles:true}));");
  await evaluate("document.querySelector('.group-check[data-staged=\"false\"]').click()");
  assert.equal(await evaluate("document.querySelectorAll('.file-check:checked').length"), 2);
  await evaluate("document.querySelector('.file-check[data-path=\"hello.txt\"]').click()");
  assert.equal(await evaluate("document.querySelector('.group-check[data-staged=\"false\"]').indeterminate"), true);
  await evaluate("document.querySelector('.file-check[data-path=\"new file.txt\"]').click()");
  await execute("document.querySelector('[data-resize=\"width\"]').dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowRight',bubbles:true}));");
  assert.ok(Number(await evaluate("localStorage.getItem('files-width')")) >= 260);

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

  gitAt(cloneParent, "clone", "--", fixture, cloned);
  await evaluate("document.querySelector('[data-action=\"library\"]').click()");
  await addLocal(cloned);
  await clickRepository(cloned);
  await waitFor("document.querySelector('.toolbar-context > strong')?.textContent === 'cloned' && !!document.querySelector('.clean-state') && !document.querySelector('#statusbar .spinner')", "打开新增本地仓库");
  assert.equal(gitAt(cloned, "remote", "get-url", "origin"), fixture);
  gitAt(cloned, "config", "user.name", "XSourceTree Smoke");
  gitAt(cloned, "config", "user.email", "smoke@example.test");
  gitAt(cloned, "config", "commit.gpgsign", "false");

  writeFileSync(join(cloned, "hello.txt"), "stash tracked\n");
  writeFileSync(join(cloned, "stash new.txt"), "stash untracked\n");
  await evaluate("document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("document.querySelectorAll('.file-row').length === 2 && !document.querySelector('#statusbar .spinner')", "储藏前刷新");
  await evaluate("document.querySelector('[data-action=\"new-stash\"]').click()");
  await execute("document.querySelector('#stash-message').value='smoke stash'; document.querySelector('#stash-form').requestSubmit()");
  await waitFor("!!document.querySelector('.clean-state') && document.querySelector('[data-action=\"stash\"] .count')?.textContent === '1' && !document.querySelector('#statusbar .spinner')", "创建含未跟踪文件的储藏");
  assert(!existsSync(join(cloned, "stash new.txt")));
  await evaluate("document.querySelector('[data-action=\"stash\"]').click()");
  await waitFor("document.querySelector('.diff-code')?.textContent.includes('+stash untracked') && !!document.querySelector('[data-action=\"apply-stash\"]')", "储藏详情");
  await evaluate("document.querySelector('[data-action=\"apply-stash\"]').click()");
  await waitFor("document.querySelector('[data-action=\"changes\"] .count')?.textContent === '2' && !document.querySelector('#statusbar .spinner')", "应用储藏");
  assert.equal(readFileSync(join(cloned, "stash new.txt"), "utf8"), "stash untracked\n");
  assert(gitAt(cloned, "stash", "list").includes("smoke stash"), "apply 应保留储藏");
  gitAt(cloned, "restore", "--worktree", ".");
  rmSync(join(cloned, "stash new.txt"));
  await evaluate("document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("document.querySelector('[data-action=\"changes\"] .count')?.textContent === '0' && !document.querySelector('#statusbar .spinner')", "恢复干净工作区");
  await evaluate("document.querySelector('[data-action=\"pop-stash\"]').click()");
  await waitFor("document.querySelector('[data-action=\"stash\"] .count')?.textContent === '0' && !document.querySelector('#statusbar .spinner')", "应用并删除储藏");
  assert.equal(gitAt(cloned, "stash", "list"), "");
  assert.equal(readFileSync(join(cloned, "hello.txt"), "utf8"), "stash tracked\n");
  await evaluate("document.querySelector('[data-action=\"new-stash\"]').click()");
  await execute("document.querySelector('#stash-message').value='drop me'; document.querySelector('#stash-form').requestSubmit()");
  await waitFor("document.querySelectorAll('.stash-row').length === 1 && !document.querySelector('#statusbar .spinner')", "创建待删除储藏");
  await evaluate("document.querySelector('[data-action=\"drop-stash\"]').click()");
  assert(gitAt(cloned, "stash", "list").includes("drop me"), "确认前不能删除");
  await evaluate("document.querySelector('#confirm-form').requestSubmit()");
  await waitFor("document.querySelectorAll('.stash-row').length === 0 && !document.querySelector('#statusbar .spinner')", "确认删除储藏");
  assert.equal(gitAt(cloned, "stash", "list"), "");

  const baseBranch = gitAt(cloned, "branch", "--show-current");
  gitAt(cloned, "switch", "-c", "conflict/topic");
  writeFileSync(join(cloned, "hello.txt"), "incoming version\n");
  gitAt(cloned, "commit", "-am", "incoming change");
  gitAt(cloned, "switch", baseBranch);
  writeFileSync(join(cloned, "hello.txt"), "current version\n");
  gitAt(cloned, "commit", "-am", "current change");
  await execute("document.querySelector('[data-action=\"changes\"]').click(); document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("!!document.querySelector('[data-action=\"switch\"][data-name=\"conflict/topic\"]') && !document.querySelector('#statusbar .spinner')", "准备合并分支");
  await evaluate("document.querySelector('[data-action=\"merge\"]').click()");
  await execute("document.querySelector('#merge-branch').value='refs/heads/conflict/topic'; document.querySelector('#merge-form').requestSubmit()");
  await waitFor("!!document.querySelector('#conflict-result') && !!document.querySelector('.operation-bar') && !document.querySelector('#statusbar .spinner')", "合并冲突编辑器");
  if (process.env.XSOURCETREE_CONFLICT_SCREENSHOT) {
    const capture = await request("capture");
    writeFileSync(process.env.XSOURCETREE_CONFLICT_SCREENSHOT, Buffer.from(capture.png, "base64"));
  }
  assert.equal(await evaluate("document.querySelector('[data-action=\"operation-continue\"]').disabled"), true, "冲突时不能继续");
  assert.equal(await evaluate("document.querySelectorAll('.file-action[data-action=\"stage\"]').length"), 0, "冲突不能普通暂存");
  await execute("document.querySelector('#conflict-result').value='keep my draft\\n'; document.querySelector('#conflict-result').dispatchEvent(new Event('input',{bubbles:true}))");
  writeFileSync(join(cloned, "hello.txt"), "changed outside editor\n");
  await evaluate("document.querySelector('[data-action=\"resolve-result\"]').click()");
  await waitFor("document.querySelector('[data-action=\"resolve-result\"]')?.disabled && document.querySelector('#alerts')?.textContent.includes('外部改变') && !document.querySelector('#statusbar .spinner')", "过期结果禁止覆盖");
  assert.equal(readFileSync(join(cloned, "hello.txt"), "utf8"), "changed outside editor\n");
  assert.equal(await evaluate("document.querySelector('#conflict-result').value"), "keep my draft\n", "过期保护应保留编辑草稿");
  await evaluate("document.querySelector('[data-action=\"reload-conflict\"]').click()");
  await evaluate("document.querySelector('#confirm-form').requestSubmit()");
  await waitFor("document.querySelector('#conflict-result')?.value === 'changed outside editor\\n' && !document.querySelector('[data-action=\"resolve-result\"]')?.disabled", "重新载入冲突");
  await execute("document.querySelector('#conflict-result').value='resolved in desktop\\n'; document.querySelector('#conflict-result').dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('[data-action=\"resolve-result\"]').click()");
  await waitFor("!document.querySelector('[data-action=\"operation-continue\"]')?.disabled && !document.querySelector('#statusbar .spinner')", "保存并标记解决");
  assert.equal(gitAt(cloned, "show", ":hello.txt"), "resolved in desktop");
  await evaluate("document.querySelector('[data-action=\"operation-continue\"]').click()");
  await waitFor("!document.querySelector('.operation-bar') && !!document.querySelector('.clean-state') && !document.querySelector('#statusbar .spinner')", "完成合并");
  assert.equal(gitAt(cloned, "log", "-1", "--format=%P").split(" ").length, 2);

  writeFileSync(join(cloned, "hello.txt"), "stash conflict version\n");
  await evaluate("document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("document.querySelectorAll('.file-row').length === 1 && !document.querySelector('#statusbar .spinner')", "储藏冲突准备");
  await evaluate("document.querySelector('[data-action=\"new-stash\"]').click()");
  await execute("document.querySelector('#stash-message').value='conflicting stash'; document.querySelector('#stash-form').requestSubmit()");
  await waitFor("!!document.querySelector('.clean-state') && !document.querySelector('#statusbar .spinner')", "创建冲突用储藏");
  writeFileSync(join(cloned, "hello.txt"), "new committed version\n");
  gitAt(cloned, "commit", "-am", "diverge from stash");
  await evaluate("document.querySelector('[data-action=\"refresh\"]').click()");
  await waitFor("!document.querySelector('#statusbar .spinner')", "刷新新提交");
  await evaluate("document.querySelector('[data-action=\"stash\"]').click()");
  await waitFor("!!document.querySelector('[data-action=\"pop-stash\"]')", "选中冲突储藏");
  await evaluate("document.querySelector('[data-action=\"pop-stash\"]').click()");
  await waitFor("document.querySelector('#alerts')?.textContent.includes('CONFLICT') && !document.querySelector('#statusbar .spinner')", "储藏冲突保留");
  assert(gitAt(cloned, "stash", "list").includes("conflicting stash"));
  await evaluate("document.querySelector('[data-action=\"changes\"]').click()");
  await waitFor("!!document.querySelector('[data-action=\"resolve-theirs\"]') && !document.querySelector('#statusbar .spinner')", "储藏冲突编辑器");
  await evaluate("document.querySelector('[data-action=\"resolve-theirs\"]').click()");
  await waitFor("document.querySelectorAll('[data-action=\"unstage\"]').length === 1 && !document.querySelector('#statusbar .spinner')", "选择储藏版本解决");
  await execute("document.querySelector('#commit-message').value='resolve stash conflict'; document.querySelector('#commit-message').dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#commit-form').requestSubmit()");
  await waitFor("!!document.querySelector('.clean-state') && !document.querySelector('#statusbar .spinner')", "提交储藏冲突解决");
  assert.equal(gitAt(cloned, "show", "HEAD:hello.txt"), "stash conflict version");
  assert.deepEqual(await evaluate("window.__smokeErrors"), [], "桌面页面不应出现未处理的脚本错误");

  if (process.env.XSOURCETREE_SCREENSHOT) {
    const capture = await request("capture");
    writeFileSync(process.env.XSOURCETREE_SCREENSHOT, Buffer.from(capture.png, "base64"));
  }
  console.log("PASS：真实 MyGo 本地仓库添加/去重、扫描勾选、分组/移动/搜索/重载持久化、移除不删文件，以及暂存/提交/分支/储藏/冲突处理");
  console.log(`测试仓库：${fixture}`);
  console.log(`克隆仓库：${cloned}`);
} finally {
  socket.end();
}
