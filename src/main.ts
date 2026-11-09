import "./style.css";
import { RepositoryService as api, events, type Branch, type Commit, type Diff, type File, type Recent, type Snapshot } from "./mygo";
import { escapeHTML as escape, fileGroups, graphRows, parseDiff, type GraphRow } from "./presentation";
import { icon } from "./icons";
import { settleMutation } from "./mutations";

type Selection = { path: string; staged: boolean } | { hash: string };
type View = "changes" | "history";

const state = {
  repo: null as Snapshot | null,
  recent: [] as Recent[],
  branches: [] as Branch[],
  commits: [] as Commit[],
  view: "changes" as View,
  selection: null as Selection | null,
  diff: null as Diff | null,
  diffLoading: false,
  diffError: "",
  busy: "",
  error: "",
  notice: "",
  query: "",
  drafts: new Map<string, string>(),
  hasMore: false,
  stale: false,
};
let selectionVersion = 0;
let pendingRefresh: Promise<void> | null = null;
let noticeTimer: ReturnType<typeof setTimeout> | undefined;
const app = document.querySelector<HTMLDivElement>("#app")!;

app.innerHTML = `
  <header class="titlebar"><div class="titlebar-brand">${icon("branch", 17)}<span>XSourceTree</span></div><div id="titlebar-context"></div><span class="version">0.1.0</span></header>
  <div class="workspace"><aside class="sidebar" aria-label="仓库与导航"></aside><main><div id="toolbar" class="toolbar"></div><div id="alerts" aria-live="polite"></div><div id="content" class="content"></div></main></div>
  <footer class="statusbar" id="statusbar" aria-live="polite"></footer>
  <div id="dialog-host"></div>
`;

const disabled = () => state.busy ? "disabled" : "";
const draft = () => state.drafts.get(state.repo?.path ?? "") ?? "";
const canCommit = () => !!state.repo && !state.busy && !state.stale && !!draft().trim()
  && fileGroups(state.repo.files).staged.length > 0 && !state.repo.files.some((file) => file.conflict);

function button(action: string, label: string, glyph: string, extra = ""): string {
  return `<button data-action="${action}" data-mutates ${disabled()} ${extra}>${icon(glyph)}<span>${label}</span></button>`;
}

function setNotice(message: string): void {
  state.notice = message;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    state.notice = "";
    renderAlerts();
  }, 4000);
}

function render(): void {
  const focused = document.activeElement;
  const focusID = focused?.id;
  const cursor: [number | null, number | null] | null = focused instanceof HTMLInputElement || focused instanceof HTMLTextAreaElement
    ? [focused.selectionStart, focused.selectionEnd] : null;
  renderSidebar();
  renderToolbar();
  renderAlerts();
  renderContent();
  if (state.stale) {
    document.querySelectorAll<HTMLButtonElement>("[data-mutates]").forEach((button) => { button.disabled = true; });
  }
  document.querySelector("#titlebar-context")!.textContent = state.repo?.name ?? "";
  document.querySelector("#statusbar")!.innerHTML = state.repo
    ? `<span>${icon("branch", 13)}${escape(state.repo.branch)}${state.repo.detached ? " · 分离的 HEAD" : ""}</span><span class="status-path" title="${escape(state.repo.path)}">${escape(state.repo.path)}</span><span>${state.busy ? `<span class="spinner"></span>${escape(state.busy)}` : `${state.repo.files.length} 个变更`}</span>`
    : `<span>未打开仓库</span><span class="status-path"></span><span>${state.busy ? escape(state.busy) : "就绪"}</span>`;
  if (focusID) {
    const target = document.getElementById(focusID);
    target?.focus();
    if (cursor && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) && cursor[0] !== null && cursor[1] !== null) {
      target.setSelectionRange(cursor[0], cursor[1]);
    }
  }
}

function renderSidebar(): void {
  const repo = state.repo;
  const groups = repo ? fileGroups(repo.files) : { staged: [], working: [] };
  document.querySelector(".sidebar")!.innerHTML = `
    <div class="sidebar-heading"><span>仓库</span><button class="icon-button" data-action="choose" title="打开仓库（⌘O）" aria-label="打开仓库" ${disabled()}>${icon("plus", 16)}</button></div>
    <div class="repo-list">${state.recent.length ? state.recent.map((entry) => `
      <button class="repo-item ${repo?.path === entry.path ? "active" : ""}" data-action="open" data-path="${escape(entry.path)}" title="${escape(entry.path)}" ${disabled()}>
        <span class="repo-avatar">${escape(entry.name.slice(0, 1).toUpperCase())}</span><span class="repo-name">${escape(entry.name)}</span>${repo?.path === entry.path ? '<span class="active-dot"></span>' : ""}
      </button>`).join("") : '<p class="sidebar-empty">尚未打开仓库</p>'}</div>
    ${repo ? `
      <div class="sidebar-section-label">工作区</div>
      <nav class="navigation">
        <button data-action="changes" class="${state.view === "changes" ? "selected" : ""}" ${disabled()}>${icon("changes", 17)}<span>文件状态</span><span class="count">${repo.files.length}</span></button>
        <button data-action="history" class="${state.view === "history" ? "selected" : ""}" ${disabled()}>${icon("history", 17)}<span>提交历史</span></button>
      </nav>
      <div class="sidebar-heading branch-heading"><span>本地分支</span><button class="icon-button" data-action="new-branch" data-mutates aria-label="新建分支" title="新建分支" ${disabled()}>${icon("plus", 15)}</button></div>
      <div class="branch-list">${state.branches.filter((branch) => !branch.remote).map((branch) => `
        <button data-action="switch" data-mutates data-name="${escape(branch.name)}" class="branch-item ${branch.current ? "current" : ""}" title="${escape(branch.name)}" ${disabled()}>
          ${icon("branch", 15)}<span>${escape(branch.name)}</span>${branch.current ? icon("check", 14) : ""}
        </button>`).join("") || `<div class="branch-item current">${icon("branch", 15)}<span>${escape(repo.branch)}</span><span class="muted">未提交</span></div>`}</div>
      ${repo.remotes.length ? `<div class="sidebar-section-label">远程</div><div class="remote-list">${repo.remotes.map((name) => `
        <div class="remote-item">${icon("cloud", 15)}<span>${escape(name)}</span></div>`).join("")}</div>` : ""}
      <div class="sidebar-bottom">${groups.staged.length ? `<span class="staged-dot"></span>${groups.staged.length} 个文件已暂存` : ""}</div>
    ` : ""}
  `;
}

function renderToolbar(): void {
  const repo = state.repo;
  document.querySelector("#toolbar")!.innerHTML = repo ? `
    <div class="toolbar-context"><strong>${escape(repo.name)}</strong><span class="branch-pill">${icon("branch", 13)}${escape(repo.branch)}</span></div>
    <div class="toolbar-actions">
      ${button("fetch", "获取", "cloud", repo.remotes.length ? 'title="获取远程更新"' : 'disabled title="请先配置远程仓库"')}
      ${button("pull", `拉取${repo.behind ? ` ${repo.behind}` : ""}`, "down", repo.upstream ? 'title="仅快进拉取"' : 'disabled title="请先在命令行设置上游分支"')}
      ${button("push", `推送${repo.ahead ? ` ${repo.ahead}` : ""}`, "up", repo.upstream ? 'title="推送到当前上游分支"' : 'disabled title="请先在命令行设置上游分支"')}
      <span class="toolbar-separator"></span>
      <button class="icon-button" data-action="refresh" title="刷新（⌘R）" aria-label="刷新仓库" ${disabled()}>${icon("refresh")}</button>
    </div>` : `<div class="toolbar-context"><strong>开始使用</strong></div><span class="muted">XSourceTree</span>`;
}

function renderAlerts(): void {
  document.querySelector("#alerts")!.innerHTML = state.error
    ? `<div class="alert error" role="alert">${icon("warning", 17)}<pre>${escape(state.error)}</pre><button class="icon-button" data-action="dismiss-error" aria-label="关闭错误">${icon("close", 15)}</button></div>`
    : state.notice ? `<div class="alert success">${icon("check", 16)}<span>${escape(state.notice)}</span></div>` : "";
}

function renderContent(): void {
  const content = document.querySelector<HTMLDivElement>("#content")!;
  if (!state.repo) {
    content.className = "content welcome-content";
    content.innerHTML = `
      <div class="welcome">
        <div class="app-mark">${icon("branch", 42)}</div>
        <h1>打开你的 Git 仓库</h1>
        <p>打开本地 Git 仓库，查看变更并开始提交。</p>
        <button class="primary open-primary" data-action="choose" ${disabled()}>${icon("folder", 18)}打开仓库…<kbd>⌘ O</kbd></button>
        <button class="text-button" data-action="open-path" ${disabled()}>输入仓库路径</button>
      </div>`;
    return;
  }
  content.className = `content ${state.view}-content`;
  if (state.view === "changes") {
    content.innerHTML = `<section class="files-panel" aria-label="文件变更">${renderFilePanel()}</section><section class="diff-panel" aria-label="差异预览">${renderDiffPanel()}</section>`;
  } else {
    content.innerHTML = `<section class="history-panel" aria-label="提交历史">${renderHistoryPanel()}</section><section class="diff-panel history-detail" aria-label="提交详情">${renderDiffPanel()}</section>`;
  }
}

function renderFilePanel(): string {
  const { staged, working } = fileGroups(state.repo!.files);
  const query = state.query.toLocaleLowerCase();
  return `
    <div class="panel-title"><h2>文件状态</h2><span class="subtle">${state.repo!.files.length} 个变更</span></div>
    <div class="search-wrap">${icon("search", 15)}<input id="filter" type="search" placeholder="筛选文件…" aria-label="筛选文件" value="${escape(state.query)}" /></div>
    <div class="file-groups">
      ${renderFileGroup("未暂存", working, false, query)}
      ${renderFileGroup("已暂存", staged, true, query)}
      ${!state.repo!.files.length ? `<div class="clean-state">${icon("check", 25)}<strong>工作区干净</strong><span>所有变更均已提交</span></div>` : ""}
    </div>
    <form id="commit-form" class="commit-form">
      <div class="commit-label"><label for="commit-message">提交说明</label><span>${staged.length} 个文件</span></div>
      <textarea id="commit-message" rows="4" placeholder="这次做了什么改动？" aria-label="提交说明" ${state.busy ? "readonly" : ""}>${escape(draft())}</textarea>
      ${state.repo!.files.some((file) => file.conflict) ? '<div class="conflict-hint">请先解决冲突并暂存文件</div>' : ""}
      <button class="primary commit-button" type="submit" ${canCommit() ? "" : "disabled"}>${icon("commit", 17)}<span>提交到 ${escape(state.repo!.branch)}</span><kbd>⌘ ↵</kbd></button>
    </form>`;
}

function renderFileGroup(label: string, files: File[], staged: boolean, query: string): string {
  const filtered = files.filter((file) => file.path.toLocaleLowerCase().includes(query));
  return `<div class="file-group">
    <div class="group-heading"><span>${label}<span class="count">${files.length}</span></span>
      ${files.length ? `<button class="small-button" data-action="${staged ? "unstage-all" : "stage-all"}" data-mutates ${disabled()}>${staged ? "全部取消" : "全部暂存"}</button>` : ""}
    </div>
    ${filtered.map((file) => {
      const selected = state.selection && "path" in state.selection && state.selection.path === file.path && state.selection.staged === staged;
      const code = file.conflict ? "!" : (staged ? file.index : file.working);
      const pathParts = file.path.split("/");
      const name = pathParts.pop()!;
      return `<div class="file-row ${selected ? "selected" : ""}">
        <button class="file-select" data-action="select-file" data-path="${escape(file.path)}" data-staged="${staged}" title="${escape(file.originalPath ? `${file.originalPath} → ${file.path}` : file.path)}" aria-pressed="${!!selected}">
          <span class="file-status status-${code === "?" || code === "A" ? "added" : code === "D" ? "removed" : file.conflict ? "conflict" : "modified"}">${escape(code === "?" ? "A" : code)}</span>
          <span class="file-name">${escape(name)}${pathParts.length ? `<small>${escape(pathParts.join("/"))}</small>` : ""}</span>
        </button>
        <button class="file-action icon-button" data-action="${staged ? "unstage" : "stage"}" data-mutates data-path="${escape(file.path)}" title="${staged ? "取消暂存" : "暂存"}" aria-label="${staged ? "取消暂存" : "暂存"} ${escape(file.path)}" ${disabled()}>${icon(staged ? "minus" : "plus", 15)}</button>
      </div>`;
    }).join("")}
    ${!filtered.length ? `<div class="group-empty">${files.length ? "没有匹配的文件" : staged ? "暂存文件后即可提交" : "没有未暂存的变更"}</div>` : ""}
  </div>`;
}

const graphColors = ["#3975d5", "#a16ad7", "#37a58d", "#d49b3c", "#da7184"];

function graphSVG(row: GraphRow): string {
  const width = Math.max(42, Math.min(160, row.columns * 14 + 14));
  const x = (column: number) => 12 + column * 14;
  return `<svg class="commit-graph" width="${width}" height="52" viewBox="0 0 ${width} 52" aria-hidden="true">
    ${row.incoming.map((column) => `<path d="M${x(column)} 0V26" stroke="${graphColors[column % graphColors.length]}" />`).join("")}
    ${row.edges.map((edge) => `<path d="M${x(edge.from)} 26C${x(edge.from)} 40 ${x(edge.to)} 38 ${x(edge.to)} 52" stroke="${graphColors[edge.from % graphColors.length]}"/>`).join("")}
    <circle cx="${x(row.column)}" cy="26" r="4" fill="${graphColors[row.column % graphColors.length]}" />
  </svg>`;
}

function renderHistoryPanel(): string {
  const rows = graphRows(state.commits);
  const query = state.query.toLocaleLowerCase();
  return `<div class="panel-title"><h2>提交历史</h2><span class="subtle">所有分支 · ${state.commits.length} 条</span></div>
    <div class="search-wrap">${icon("search", 15)}<input id="filter" type="search" placeholder="搜索已加载的提交…" aria-label="搜索已加载的提交" value="${escape(state.query)}" /></div>
    <div class="commit-list">${state.commits.map((commit, index) => {
      if (query && !`${commit.subject} ${commit.author} ${commit.hash}`.toLocaleLowerCase().includes(query)) return "";
      const selected = state.selection && "hash" in state.selection && state.selection.hash === commit.hash;
      return `<button class="commit-row ${selected ? "selected" : ""}" data-action="select-commit" data-hash="${commit.hash}">
        ${query ? `<span class="search-commit-icon">${icon("commit", 17)}</span>` : graphSVG(rows[index]!)}
        <span class="commit-info"><span class="commit-subject">${escape(commit.subject || "（无提交说明）")}</span>
          <span class="commit-meta"><span>${escape(commit.author)}</span><time>${escape(formatDate(commit.date))}</time>${commit.refs ? `<span class="ref-label" title="${escape(commit.refs)}">${escape(commit.refs)}</span>` : ""}</span></span>
        <code>${commit.hash.slice(0, 7)}</code></button>`;
    }).join("") || `<div class="empty-state">${icon("history", 30)}<strong>${state.repo!.unborn ? "还没有提交" : "没有匹配的提交"}</strong></div>`}
    ${state.hasMore ? `<button class="load-more" data-action="more" ${disabled()}>加载更早的提交</button>` : ""}</div>`;
}

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date);
}

function renderDiffPanel(): string {
  const selection = state.selection;
  if (!selection) return `<div class="empty-state">${icon(state.view === "changes" ? "changes" : "commit", 36)}<strong>${state.view === "changes" ? "选择文件查看变更" : "选择提交查看详情"}</strong></div>`;
  const title = "path" in selection ? selection.path : selection.hash.slice(0, 12);
  const subtitle = "path" in selection ? selection.staged ? "HEAD → 暂存区" : "工作区变更" : "提交详情";
  let body = "";
  if (state.diffLoading) body = '<div class="empty-state"><span class="spinner large"></span><span>正在读取差异…</span></div>';
  else if (state.diffError) body = `<div class="diff-error" role="alert">${escape(state.diffError)}</div>`;
  else if (!state.diff?.text) body = '<div class="empty-state"><span>没有可显示的文本差异</span></div>';
  else {
    const rows = parseDiff(state.diff.text);
    body = `<div class="diff-scroll"><div class="diff-code" role="region" aria-label="统一差异" tabindex="0">${rows.slice(0, 4000).map((row) => `
      <div class="diff-line ${row.kind}"><span class="line-number" aria-hidden="true">${row.before ?? ""}</span><span class="line-number" aria-hidden="true">${row.after ?? ""}</span><code>${escape(row.content) || " "}</code></div>`).join("")}</div>
      ${state.diff.truncated || rows.length > 4000 ? '<div class="diff-limit">预览已截断，请使用命令行查看完整差异。</div>' : ""}</div>`;
  }
  return `<div class="diff-heading">${icon("file", 16)}<strong title="${escape(title)}">${escape(title)}</strong><span>${subtitle}</span>${state.diff?.binary ? '<span class="badge">二进制</span>' : ""}</div>${body}`;
}

async function loadSelection(): Promise<void> {
  const version = ++selectionVersion;
  const selection = state.selection;
  const repo = state.repo;
  state.diff = null;
  state.diffError = "";
  state.diffLoading = !!selection;
  renderDiffOnly();
  if (!selection || !repo) return;
  try {
    const diff = "path" in selection
      ? await api.diff(repo.path, selection.path, selection.staged)
      : await api.commitDiff(repo.path, selection.hash);
    if (version === selectionVersion) state.diff = diff;
  } catch (error) {
    if (version === selectionVersion) state.diffError = String(error instanceof Error ? error.message : error);
  } finally {
    if (version === selectionVersion) {
      state.diffLoading = false;
      renderDiffOnly();
    }
  }
}

function renderDiffOnly(): void {
  const panel = document.querySelector(".diff-panel");
  if (panel) panel.innerHTML = renderDiffPanel();
}

function reconcileSelection(): void {
  if (!state.repo) {
    state.selection = null;
    return;
  }
  if (state.view === "history") {
    if (!state.selection || !("hash" in state.selection) || !state.commits.some((commit) => commit.hash === (state.selection as { hash: string }).hash)) {
      state.selection = state.commits[0] ? { hash: state.commits[0].hash } : null;
    }
    return;
  }
  const groups = fileGroups(state.repo.files);
  const selection = state.selection;
  if (selection && "path" in selection && (selection.staged ? groups.staged : groups.working).some((file) => file.path === selection.path)) return;
  const first = groups.working[0] ?? groups.staged[0];
  state.selection = first ? { path: first.path, staged: !groups.working.length } : null;
}

function refresh(): Promise<void> {
  if (pendingRefresh) return pendingRefresh;
  pendingRefresh = refreshRepository().catch((error) => {
    state.stale = true;
    throw error;
  }).finally(() => { pendingRefresh = null; });
  return pendingRefresh;
}

async function refreshRepository(): Promise<void> {
  const path = state.repo?.path;
  if (!path) return;
  const [repo, branches, commits] = await Promise.all([api.snapshot(path), api.branches(path), api.history(path, 0)]);
  if (state.repo?.path !== path) return;
  state.repo = repo;
  state.branches = branches;
  state.commits = commits;
  state.hasMore = commits.length === 100;
  state.stale = false;
  reconcileSelection();
  render();
  await loadSelection();
}

async function perform(label: string, operation: () => Promise<void>): Promise<void> {
  if (state.busy) return;
  state.busy = label;
  state.error = "";
  state.notice = "";
  render();
  try {
    if (pendingRefresh) await pendingRefresh;
    await operation();
  } catch (error) {
    state.error = error instanceof Error ? error.message : String(error);
  } finally {
    state.busy = "";
    render();
  }
}

async function openRepository(path?: string): Promise<void> {
  await perform("正在打开仓库…", async () => {
    const repo = path ? await api.open(path) : await api.choose();
    if (!repo) return;
    ++selectionVersion;
    state.repo = repo;
    state.selection = null;
    state.diff = null;
    state.query = "";
    state.view = "changes";
    state.recent = await api.recent();
    await refresh();
  });
}

async function mutate(label: string, operation: (path: string) => Promise<void>, success?: string): Promise<void> {
  const path = state.repo?.path;
  if (!path || state.stale) return;
  await perform(label, async () => {
    const result = await settleMutation(() => operation(path), refresh);
    if (result.refreshError) {
      const original = result.operationError instanceof Error ? result.operationError.message : result.operationError ? String(result.operationError) : "";
      const reason = result.refreshError instanceof Error ? result.refreshError.message : String(result.refreshError);
      throw new Error(`${original ? original + "\n" : ""}读取仓库状态失败，请刷新后再操作：${reason}`);
    }
    if (result.operationError) throw result.operationError;
    if (success) setNotice(success);
  });
}

function showInputDialog(kind: "path" | "branch"): void {
  if (state.busy) return;
  const branch = kind === "branch";
  document.querySelector("#dialog-host")!.innerHTML = `
    <dialog id="input-dialog" class="input-dialog" aria-labelledby="dialog-title">
      <form id="input-form" data-kind="${kind}">
        <h2 id="dialog-title">${branch ? "新建分支" : "打开仓库"}</h2>
        <label for="dialog-input">${branch ? "分支名称" : "本地仓库路径"}</label>
        <input id="dialog-input" name="value" placeholder="${branch ? "feature/my-change" : "/Users/you/Projects/repository"}" required autocomplete="off" spellcheck="false" />
        <div class="dialog-actions"><button type="button" data-action="cancel-dialog">取消</button><button class="primary" type="submit">${branch ? "创建并切换" : "打开"}</button></div>
      </form>
    </dialog>`;
  document.querySelector<HTMLDialogElement>("#input-dialog")!.showModal();
  document.querySelector<HTMLInputElement>("#dialog-input")!.focus();
}

async function commit(): Promise<void> {
  if (!canCommit()) return;
  const path = state.repo!.path;
  const message = draft();
  await mutate("正在提交…", async () => {
    await api.commit(path, message);
    state.drafts.delete(path);
  }, "提交成功");
}

document.addEventListener("click", (event) => {
  const target = (event.target as Element).closest<HTMLButtonElement>("button[data-action]");
  if (!target || target.disabled) return;
  const action = target.dataset.action!;
  if (action === "cancel-dialog") return document.querySelector<HTMLDialogElement>("dialog")?.close();
  if (action === "dismiss-error") {
    state.error = "";
    renderAlerts();
    return;
  }
  if (state.busy) return;
  switch (action) {
    case "choose": void openRepository(); break;
    case "open-path": showInputDialog("path"); break;
    case "open": void openRepository(target.dataset.path); break;
    case "new-branch": showInputDialog("branch"); break;
    case "refresh": void perform("正在刷新…", refresh); break;
    case "changes":
    case "history":
      state.view = action;
      state.query = "";
      reconcileSelection();
      render();
      void loadSelection();
      break;
    case "select-file":
      state.selection = { path: target.dataset.path!, staged: target.dataset.staged === "true" };
      render();
      void loadSelection();
      break;
    case "select-commit":
      state.selection = { hash: target.dataset.hash! };
      render();
      void loadSelection();
      break;
    case "stage":
    case "unstage": {
      const method = action === "stage" ? api.stage : api.unstage;
      void mutate(action === "stage" ? "正在暂存…" : "正在取消暂存…", (path) => method(path, [target.dataset.path!]));
      break;
    }
    case "stage-all":
    case "unstage-all": {
      if (!state.repo) break;
      const groups = fileGroups(state.repo.files);
      const paths = (action === "stage-all" ? groups.working : groups.staged).map((file) => file.path);
      const method = action === "stage-all" ? api.stage : api.unstage;
      void mutate("正在更新暂存区…", (path) => method(path, paths));
      break;
    }
    case "switch":
      void mutate("正在切换分支…", (path) => api.switchBranch(path, target.dataset.name!), "分支已切换");
      break;
    case "fetch":
    case "pull":
    case "push":
      void mutate(`${action === "fetch" ? "正在获取" : action === "pull" ? "正在拉取" : "正在推送"}…`, (path) => api.remote(path, action), "远程操作完成");
      break;
    case "more":
      void perform("正在加载提交…", async () => {
        const commits = await api.history(state.repo!.path, state.commits.length);
        state.commits.push(...commits);
        state.hasMore = commits.length === 100;
      });
      break;
  }
});

document.addEventListener("input", (event) => {
  const target = event.target as HTMLInputElement | HTMLTextAreaElement;
  if (target.id === "commit-message") {
    state.drafts.set(state.repo!.path, target.value);
    document.querySelector<HTMLButtonElement>(".commit-button")!.disabled = !canCommit();
  }
  if (target.id === "filter") {
    state.query = target.value;
    render();
  }
});

document.addEventListener("submit", (event) => {
  const form = event.target as HTMLFormElement;
  event.preventDefault();
  if (form.id === "commit-form") void commit();
  if (form.id === "input-form") {
    const value = String(new FormData(form).get("value") ?? "").trim();
    if (!value) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    if (form.dataset.kind === "path") void openRepository(value);
    else void mutate("正在创建分支…", (path) => api.createBranch(path, value), "分支已创建");
  }
});

document.addEventListener("keydown", (event) => {
  if (!(event.metaKey || event.ctrlKey)) return;
  if (event.key === "Enter") {
    event.preventDefault();
    void commit();
  }
});

window.addEventListener("focus", () => {
  if (state.repo && !state.busy && !document.querySelector("dialog[open]")) {
    void refresh().catch((error) => {
      state.error = error instanceof Error ? error.message : String(error);
      render();
    });
  }
});

events.repositoryOpen.on(() => { void openRepository(); });
events.repositoryRefresh.on(() => { void perform("正在刷新…", refresh); });
window.addEventListener("beforeunload", () => clearTimeout(noticeTimer));

render();
void api.recent().then((recent) => {
  state.recent = recent;
  render();
}).catch((error) => {
  state.error = error instanceof Error ? error.message : String(error);
  renderAlerts();
});
