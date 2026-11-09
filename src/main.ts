import "./style.css";
import { RepositoryService as api, events, type Branch, type Commit, type Conflict, type ConflictSide, type Diff, type File, type Library, type LocalRepository, type Patch, type ScanResult, type Snapshot, type Stash } from "./mygo";
import { escapeHTML as escape, fileGroups, graphRows, parseDiff, type GraphRow } from "./presentation";
import { icon } from "./icons";
import { settleMutation } from "./mutations";
import { filterRepositories } from "./library";
import { checkedPaths, visibleFiles, reconcileChecked, type FileSort } from "./files";

type Selection = { path: string; staged: boolean } | { hash: string } | { stash: string };
type View = "changes" | "history" | "stash";

const state = {
  repo: null as Snapshot | null,
  library: { repositories: [], groups: [] } as Library,
  screen: "library" as "library" | "repository",
  libraryGroup: "ALL",
  libraryQuery: "",
  branches: [] as Branch[],
  commits: [] as Commit[],
  stashes: [] as Stash[],
  conflict: null as Conflict | null,
  conflictDrafts: new Map<string, { token: string; content: string }>(),
  view: "changes" as View,
  selection: null as Selection | null,
  diff: null as Diff | null,
  patch: null as Patch | null,
  diffLoading: false,
  diffError: "",
  busy: "",
  error: "",
  notice: "",
  query: "",
  fileSort: "path" as FileSort,
  checked: { staged: new Set<string>(), working: new Set<string>() },
  drafts: new Map<string, string>(),
  hasMore: false,
  stale: false,
};
let selectionVersion = 0;
let pendingRefresh: Promise<void> | null = null;
let noticeTimer: ReturnType<typeof setTimeout> | undefined;
let pendingConfirm: (() => void) | null = null;
const app = document.querySelector<HTMLDivElement>("#app")!;
const layout = { width: readSize("files-width", 380, 260, 700), split: readSize("files-split", 45, 15, 85) };

function readSize(key: string, fallback: number, min: number, max: number): number {
  try {
    const raw = localStorage.getItem(key);
    const value = raw === null ? fallback : Number(raw);
    return Number.isFinite(value) ? Math.max(min, Math.min(max, value)) : fallback;
  } catch { return fallback; }
}

app.innerHTML = `
  <header class="titlebar"><div class="titlebar-brand">${icon("branch", 17)}<span>XSourceTree</span></div><div id="titlebar-context"></div><span class="version">0.1.0</span></header>
  <div class="workspace"><aside class="sidebar" aria-label="仓库与导航"></aside><main><div id="toolbar" class="toolbar"></div><div id="alerts" aria-live="polite"></div><div id="content" class="content"></div></main></div>
  <footer class="statusbar" id="statusbar" aria-live="polite"></footer>
  <div id="dialog-host"></div>
`;

const disabled = () => state.busy ? "disabled" : "";
const draft = () => state.drafts.get(state.repo?.path ?? "") ?? "";
const canCommit = () => !!state.repo && !state.busy && !state.stale && !!draft().trim()
  && !state.repo.operation.kind && fileGroups(state.repo.files).staged.length > 0 && !state.repo.files.some((file) => file.conflict);
const conflictKey = () => `${state.repo?.path}\0${state.conflict?.path}`;
const operationNames: Record<string, string> = { merge: "合并", rebase: "变基", "cherry-pick": "拣选", revert: "还原" };

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
  const inRepository = state.screen === "repository" && state.repo;
  document.querySelector("#titlebar-context")!.textContent = inRepository ? state.repo!.name : "本地仓库";
  document.querySelector("#statusbar")!.innerHTML = inRepository
    ? `<span>${icon("branch", 13)}${escape(state.repo!.branch)}${state.repo!.detached ? " · 分离的 HEAD" : ""}</span><span class="status-path" title="${escape(state.repo!.path)}">${escape(state.repo!.path)}</span><span>${state.busy ? `<span class="spinner"></span>${escape(state.busy)}` : `${state.repo!.files.length} 个变更`}</span>`
    : `<span>${state.library.repositories.length} 个本地仓库</span><span class="status-path"></span><span>${state.busy ? `<span class="spinner"></span>${escape(state.busy)}` : "就绪"}</span>`;
  if (state.busy === "正在扫描文件夹…") {
    document.querySelector("#statusbar")!.insertAdjacentHTML("beforeend", '<button class="small-button" data-action="cancel-scan">取消扫描</button>');
  }
  if (focusID) {
    const target = document.getElementById(focusID);
    target?.focus();
    if (cursor && (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) && cursor[0] !== null && cursor[1] !== null) {
      target.setSelectionRange(cursor[0], cursor[1]);
    }
  }
}

function renderSidebar(): void {
  const repo = state.screen === "repository" ? state.repo : null;
  const groups = repo ? fileGroups(repo.files) : { staged: [], working: [] };
  document.querySelector(".sidebar")!.innerHTML = `
    <nav class="navigation library-navigation">
      <button data-action="library" class="${!repo ? "selected" : ""}" ${disabled()}>${icon("folder", 17)}<span>本地仓库</span><span class="count">${state.library.repositories.length}</span></button>
    </nav>
    ${!repo ? `
      <div class="sidebar-heading branch-heading"><span>仓库组</span><button class="icon-button" data-action="new-group" aria-label="新建仓库组" title="新建仓库组" ${disabled()}>${icon("plus", 15)}</button></div>
      <nav class="navigation group-navigation">
        ${renderGroupNavigation("ALL", "所有仓库", state.library.repositories.length)}
        ${renderGroupNavigation("", "未分组", state.library.repositories.filter((entry) => !entry.groupID).length)}
        ${state.library.groups.map((group) => renderGroupNavigation(group.id, group.name, state.library.repositories.filter((entry) => entry.groupID === group.id).length)).join("")}
      </nav>` : ""}
    ${repo ? `
      <div class="sidebar-repository" title="${escape(repo.path)}"><span class="repo-avatar">${icon("branch", 17)}</span><strong>${escape(repo.name)}</strong></div>
      <div class="sidebar-section-label">工作区</div>
      <nav class="navigation">
        <button data-action="changes" class="${state.view === "changes" ? "selected" : ""}" ${disabled()}>${icon("changes", 17)}<span>文件状态</span><span class="count">${repo.files.length}</span></button>
        <button data-action="history" class="${state.view === "history" ? "selected" : ""}" ${disabled()}>${icon("history", 17)}<span>提交历史</span></button>
        <button data-action="stash" class="${state.view === "stash" ? "selected" : ""}" ${disabled()}>${icon("stash", 17)}<span>储藏</span><span class="count">${state.stashes.length}</span></button>
      </nav>
      <div class="sidebar-heading branch-heading"><span>本地分支</span><button class="icon-button" data-action="new-branch" data-mutates aria-label="新建分支" title="新建分支" ${disabled()}>${icon("plus", 15)}</button></div>
      <div class="branch-list">${state.branches.filter((branch) => !branch.remote).map((branch) => `
        <button data-action="switch" data-mutates data-name="${escape(branch.name)}" class="branch-item ${branch.current ? "current" : ""}" title="${escape(branch.name)}" ${disabled()}>
          ${icon("branch", 15)}<span>${escape(branch.name)}</span>${branch.current ? icon("check", 14) : ""}
        </button>`).join("") || `<div class="branch-item current">${icon("branch", 15)}<span>${escape(repo.branch)}</span><span class="muted">未提交</span></div>`}</div>
      <div class="sidebar-bottom">${groups.staged.length ? `<span class="staged-dot"></span>${groups.staged.length} 个文件已暂存` : ""}</div>
    ` : ""}
  `;
}

function renderGroupNavigation(id: string, name: string, count: number): string {
  return `<button data-action="library-group" data-group="${escape(id)}" class="${state.libraryGroup === id ? "selected" : ""}" title="${escape(name)}" ${disabled()}>
    ${icon("folder", 15)}<span class="group-name">${escape(name)}</span><span class="count">${count}</span></button>`;
}

function libraryGroupName(): string {
  return state.libraryGroup === "ALL" ? "所有仓库" : state.library.groups.find((group) => group.id === state.libraryGroup)?.name ?? "未分组";
}

function renderToolbar(): void {
  const repo = state.repo;
  if (state.screen === "library") {
    document.querySelector("#toolbar")!.innerHTML = `
      <div class="toolbar-context"><strong>本地仓库</strong></div>
      <div class="library-toolbar-actions">
        <div class="search-wrap library-search">${icon("search", 15)}<input id="repo-search" type="search" placeholder="搜索仓库…" aria-label="搜索仓库" value="${escape(state.libraryQuery)}"></div>
        <details class="popup-menu add-menu"><summary class="menu-trigger" aria-label="新增" ${state.busy ? 'aria-disabled="true"' : ""}>${icon("plus", 16)}新增${icon("chevron", 12)}</summary>
          <div class="popup-items">
            <button data-action="choose-local" ${disabled()}>添加已有的本地仓库…</button>
            <button data-action="open-path" ${disabled()}>输入本地仓库路径…</button>
            <button data-action="scan" ${disabled()}>扫描文件夹…</button>
            <hr><button data-action="new-group" ${disabled()}>新建仓库组…</button>
          </div>
        </details>
        <button class="icon-button" data-action="refresh" title="刷新（⌘R）" aria-label="刷新本地仓库" ${disabled()}>${icon("refresh")}</button>
      </div>`;
    return;
  }
  document.querySelector("#toolbar")!.innerHTML = repo ? `
    <div class="toolbar-context"><strong>${escape(repo.name)}</strong><span class="branch-pill">${icon("branch", 13)}${escape(repo.branch)}</span></div>
    <div class="toolbar-actions">
      ${button("fetch", "获取", "cloud", repo.remotes.length ? 'title="获取远程更新"' : 'disabled title="请先配置远程仓库"')}
      ${button("pull", `拉取${repo.behind ? ` ${repo.behind}` : ""}`, "down", repo.upstream ? 'title="仅快进拉取"' : 'disabled title="请先在命令行设置上游分支"')}
      ${button("push", `推送${repo.ahead ? ` ${repo.ahead}` : ""}`, "up", repo.upstream ? 'title="推送到当前上游分支"' : 'disabled title="请先在命令行设置上游分支"')}
      ${button("new-stash", "储藏", "stash", repo.files.length && !repo.operation.kind && !repo.files.some((file) => file.conflict) ? "" : "disabled")}
      ${button("merge", "合并", "merge", !repo.unborn && !repo.operation.kind && !repo.files.length ? "" : 'disabled title="请先提交或储藏变更"')}
      <span class="toolbar-separator"></span>
      <button class="icon-button" data-action="refresh" title="刷新（⌘R）" aria-label="刷新仓库" ${disabled()}>${icon("refresh")}</button>
    </div>` : `<div class="toolbar-context"><strong>开始使用</strong></div><span class="muted">XSourceTree</span>`;
}

function renderAlerts(): void {
  const operation = state.screen === "repository" ? state.repo?.operation : null;
  const operationBar = operation?.kind ? `<div class="operation-bar"><strong>正在${escape(operationNames[operation.kind] ?? operation.kind)}</strong><span>${state.repo!.files.filter((file) => file.conflict).length} 个冲突</span>
    <button class="small-button" data-action="operation-continue" data-mutates ${state.busy || state.stale || state.repo!.files.some((file) => file.conflict) ? "disabled" : ""}>继续</button>
    ${operation.kind !== "merge" ? `<button class="small-button" data-action="operation-skip" data-mutates ${disabled()}>跳过当前提交</button>` : ""}
    <button class="small-button danger-text" data-action="operation-abort" data-mutates ${disabled()}>中止…</button></div>` : "";
  document.querySelector("#alerts")!.innerHTML = operationBar + (state.error
    ? `<div class="alert error" role="alert">${icon("warning", 17)}<pre>${escape(state.error)}</pre><button class="icon-button" data-action="dismiss-error" aria-label="关闭错误">${icon("close", 15)}</button></div>`
    : state.notice ? `<div class="alert success">${icon("check", 16)}<span>${escape(state.notice)}</span></div>` : "");
}

function renderContent(): void {
  const content = document.querySelector<HTMLDivElement>("#content")!;
  if (state.screen === "library" || !state.repo) {
    content.className = "content library-content";
    content.innerHTML = renderLibrary();
    return;
  }
  content.className = `content ${state.view}-content`;
  if (state.view === "changes") {
    content.style.setProperty("--files-width", `${layout.width}px`);
    content.style.setProperty("--files-split", `${layout.split}fr`);
    content.style.setProperty("--working-split", `${100 - layout.split}fr`);
    content.innerHTML = `<section class="files-panel" aria-label="文件变更">${renderFilePanel()}</section>
      <div class="panel-divider" role="separator" aria-label="调整文件面板宽度" aria-orientation="vertical" tabindex="0" data-resize="width"></div>
      <section class="diff-panel" aria-label="差异预览">${renderDiffPanel()}</section>${renderCommitForm()}`;
    updateFileChecks();
  } else if (state.view === "history") {
    content.innerHTML = `<section class="history-panel" aria-label="提交历史">${renderHistoryPanel()}</section><section class="diff-panel history-detail" aria-label="提交详情">${renderDiffPanel()}</section>`;
  } else {
    content.innerHTML = `<section class="history-panel" aria-label="储藏列表">${renderStashPanel()}</section><section class="diff-panel history-detail" aria-label="储藏详情">${renderDiffPanel()}</section>`;
  }
}

function renderLibrary(): string {
  const entries = filterRepositories(state.library.repositories, state.library.groups, state.libraryGroup, state.libraryQuery);
  const group = state.library.groups.find((entry) => entry.id === state.libraryGroup);
  return `<div class="library-heading"><h1>${escape(libraryGroupName())}<span class="count">${entries.length}</span></h1>
    ${group ? `<div class="library-group-actions"><button class="small-button" data-action="rename-group" ${disabled()}>重命名…</button><button class="small-button" data-action="remove-group" ${disabled()}>移除组…</button></div>` : ""}
    </div>
    <div class="library-list" aria-label="本地仓库列表">
      ${entries.map(renderLocalRepository).join("")}
      ${!entries.length ? `<div class="library-empty">${icon("folder", 34)}<h2>${state.libraryQuery.trim() ? "没有匹配的仓库" : "暂无仓库"}</h2>
        ${state.libraryQuery.trim() ? `<button data-action="clear-repo-search" ${disabled()}>清除搜索</button>` : `<div><button class="primary" data-action="choose-local" ${disabled()}>添加本地仓库…</button><button data-action="scan" ${disabled()}>扫描文件夹…</button></div>`}</div>` : ""}
    </div>`;
}

function renderLocalRepository(entry: LocalRepository): string {
  const group = state.library.groups.find((group) => group.id === entry.groupID);
  return `<div class="library-row" data-repository="${escape(entry.path)}">
    <button class="library-open" data-action="open" data-path="${escape(entry.path)}" title="${escape(entry.problem || entry.path)}" ${disabled()}>
      <span class="library-avatar">${icon("branch", 22)}</span>
      <span class="library-repository-name"><strong>${escape(entry.name)}</strong><small>${escape(entry.path)}</small></span>
      ${group && state.libraryGroup === "ALL" ? `<span class="library-group-tag">${escape(group.name)}</span>` : ""}
      ${entry.problem ? `<span class="repository-problem" title="${escape(entry.problem)}">${icon("warning", 15)}无法读取</span>` : `${entry.changes ? `<span class="change-count" title="${entry.changes} 个变更">${entry.changes}</span>` : ""}${entry.branch ? `<span class="branch-pill">${icon("branch", 12)}${escape(entry.branch)}</span>` : ""}`}
    </button>
    <details class="popup-menu repository-menu"><summary class="icon-button menu-trigger" aria-label="管理 ${escape(entry.name)}">${icon("more", 17)}</summary>
      <div class="popup-items"><button data-action="move-repository" data-path="${escape(entry.path)}" ${disabled()}>移至仓库组…</button>
        <button data-action="remove-repository" data-path="${escape(entry.path)}" ${disabled()}>从列表移除…</button></div>
    </details>
  </div>`;
}
function renderFilePanel(): string {
  const { staged, working } = fileGroups(state.repo!.files);
  return `
    <div class="panel-title"><h2>文件状态</h2><select id="file-sort" aria-label="文件排序"><option value="path" ${state.fileSort === "path" ? "selected" : ""}>按路径排序</option><option value="name" ${state.fileSort === "name" ? "selected" : ""}>按名称排序</option></select></div>
    <div class="search-wrap">${icon("search", 15)}<input id="filter" type="search" placeholder="筛选文件…" aria-label="筛选文件" value="${escape(state.query)}" /></div>
    <div class="file-groups">
      ${renderFileGroup("已暂存", staged, true)}
      <div class="panel-divider horizontal" role="separator" aria-label="调整暂存区高度" aria-orientation="horizontal" tabindex="0" data-resize="split"></div>
      ${renderFileGroup("未暂存", working, false)}
    </div>`;
}

function renderCommitForm(): string {
  const { staged } = fileGroups(state.repo!.files);
  return `
    <form id="commit-form" class="commit-form">
      <div class="commit-label"><label for="commit-message">提交说明</label><span>${staged.length} 个文件</span></div>
      <textarea id="commit-message" rows="2" placeholder="提交说明（⌘ ↵ 提交）" aria-label="提交说明" ${state.busy ? "readonly" : ""}>${escape(draft())}</textarea>
      ${state.repo!.files.some((file) => file.conflict) ? '<div class="conflict-hint">选择冲突文件以解决冲突</div>' : ""}
      <button class="primary commit-button" type="submit" ${canCommit() ? "" : "disabled"}>${icon("commit", 17)}<span>提交到 ${escape(state.repo!.branch)}</span><kbd>⌘ ↵</kbd></button>
    </form>`;
}

function renderFileGroup(label: string, files: File[], staged: boolean): string {
  const filtered = visibleFiles(files, state.query, state.fileSort);
  const actionable = filtered.filter((file) => !file.conflict);
  const checked = staged ? state.checked.staged : state.checked.working;
  const count = checkedPaths(filtered, checked).length;
  return `<div class="file-group" data-group="${staged ? "staged" : "working"}">
    <div class="group-heading"><label><input class="group-check" type="checkbox" data-staged="${staged}" aria-label="全选${label}的匹配文件" ${count > 0 && count === actionable.length ? "checked" : ""} ${!actionable.length || state.busy || state.stale ? "disabled" : ""} />${label}<span class="count">${filtered.length}</span></label>
      ${actionable.length ? `<button class="small-button" data-action="${staged ? "unstage-all" : "stage-all"}" data-mutates ${disabled()}>${count ? `${staged ? "取消暂存" : "暂存"}所选 (${count})` : staged ? "取消匹配文件暂存" : "暂存匹配文件"}</button>` : ""}
    </div>
    <div class="group-files">
    ${filtered.map((file) => {
      const selected = state.selection && "path" in state.selection && state.selection.path === file.path && state.selection.staged === staged;
      const code = file.conflict ? "!" : (staged ? file.index : file.working);
      const pathParts = file.path.split("/");
      const name = pathParts.pop()!;
      return `<div class="file-row ${selected ? "selected" : ""}">
        <input class="file-check" type="checkbox" data-path="${escape(file.path)}" data-staged="${staged}" aria-label="选择${staged ? "已暂存" : "未暂存"}文件 ${escape(file.path)}" ${checked.has(file.path) ? "checked" : ""} ${file.conflict || state.busy || state.stale ? "disabled" : ""} />
        <button class="file-select" data-action="select-file" data-path="${escape(file.path)}" data-staged="${staged}" title="${escape(file.originalPath ? `${file.originalPath} → ${file.path}` : file.path)}" aria-pressed="${!!selected}">
          <span class="file-status status-${code === "?" || code === "A" ? "added" : code === "D" ? "removed" : file.conflict ? "conflict" : "modified"}">${escape(code === "?" ? "A" : code)}</span>
          <span class="file-name">${escape(name)}${pathParts.length ? `<small>${escape(pathParts.join("/"))}</small>` : ""}</span>
        </button>
        ${file.conflict ? '<span class="conflict-label">冲突</span>' : `<button class="file-action icon-button" data-action="${staged ? "unstage" : "stage"}" data-mutates data-path="${escape(file.path)}" title="${staged ? "取消暂存" : "暂存"}" aria-label="${staged ? "取消暂存" : "暂存"} ${escape(file.path)}" ${disabled()}>${icon(staged ? "minus" : "plus", 15)}</button>`}
      </div>`;
    }).join("")}
    ${!filtered.length ? !staged && !state.repo!.files.length
      ? `<div class="clean-state">${icon("check", 25)}<strong>工作区干净</strong></div>`
      : `<div class="group-empty">${files.length ? "没有匹配的文件" : staged ? "暂存文件后即可提交" : "没有未暂存的变更"}</div>` : ""}
  </div></div>`;
}

function updateFileChecks(): void {
  if (!state.repo) return;
  const groups = fileGroups(state.repo.files);
  document.querySelectorAll<HTMLInputElement>(".group-check").forEach((input) => {
    const staged = input.dataset.staged === "true";
    const files = visibleFiles(staged ? groups.staged : groups.working, state.query, state.fileSort);
    const checked = staged ? state.checked.staged : state.checked.working;
    const count = checkedPaths(files, checked).length;
    const total = files.filter((file) => !file.conflict).length;
    input.checked = count > 0 && count === total;
    input.indeterminate = count > 0 && count < total;
    const group = input.closest(".file-group")!;
    group.querySelectorAll<HTMLInputElement>(".file-check").forEach((checkbox) => {
      checkbox.checked = checked.has(checkbox.dataset.path!);
    });
    const action = group.querySelector<HTMLButtonElement>("[data-action$='stage-all']");
    if (action) action.textContent = count ? `${staged ? "取消暂存" : "暂存"}所选 (${count})` : staged ? "取消匹配文件暂存" : "暂存匹配文件";
  });
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

function renderStashPanel(): string {
  return `<div class="panel-title"><h2>储藏</h2><button class="small-button" data-action="new-stash" data-mutates ${state.repo!.files.length && !state.repo!.operation.kind ? disabled() : "disabled"}>新建储藏…</button></div>
    <div class="commit-list">${state.stashes.map((stash) => `<button class="stash-row ${state.selection && "stash" in state.selection && state.selection.stash === stash.hash ? "selected" : ""}" data-action="select-stash" data-hash="${stash.hash}">
      ${icon("stash", 20)}<span><strong>${escape(stash.message)}</strong><small>${escape(formatDate(stash.date))} · ${stash.hash.slice(0, 8)}</small></span></button>`).join("") || '<div class="empty-state"><strong>没有储藏</strong><span>储藏工作区变更后，可以随时恢复</span></div>'}</div>`;
}

function renderConflictSide(side: ConflictSide, title: string, choice?: string): string {
  return `<section class="conflict-side"><div class="conflict-side-heading"><strong>${title}</strong>${choice ? `<button class="small-button" data-action="resolve-${choice}" data-mutates ${disabled()}>${side.present ? "保留此版本" : "按此版本删除"}</button>` : ""}</div>
    ${!side.present ? '<div class="conflict-unavailable">此版本不存在（删除）</div>' : side.binary ? '<div class="conflict-unavailable">二进制或大文件</div>' : `<pre tabindex="0">${escape(side.text)}</pre>`}</section>`;
}

function renderConflictPanel(view: Conflict): string {
  const saved = state.conflictDrafts.get(conflictKey());
  const stale = saved && saved.token !== view.token;
  const rebase = state.repo?.operation.kind === "rebase";
  return `<div class="diff-heading">${icon("warning", 16)}<strong>${escape(view.path)}</strong><span>解决冲突</span></div>
    <div class="conflict-scroll">
      ${rebase ? '<p class="conflict-explanation">变基时，“当前版本”是目标分支，“传入版本”是正在重放的提交。</p>' : ""}
      <details class="conflict-base"><summary>共同祖先</summary>${renderConflictSide(view.base, "基础版本")}</details>
      <div class="conflict-sides">${renderConflictSide(view.ours, "当前版本 · ours", "ours")}${renderConflictSide(view.theirs, "传入版本 · theirs", "theirs")}</div>
      ${stale ? '<p class="conflict-hint">文件已在外部改变，编辑草稿已保留。重新载入后再保存。</p>' : ""}
      ${view.editable ? `<label class="conflict-result-label" for="conflict-result">合并结果</label><textarea id="conflict-result" spellcheck="false" ${state.busy ? "readonly" : ""}>${escape(saved?.content ?? view.result)}</textarea>` : '<p class="conflict-explanation">请选择一个完整版本，或删除文件。</p>'}
      <div class="conflict-actions"><button class="danger-text" data-action="resolve-delete" data-mutates ${disabled()}>删除文件…</button><button data-action="reload-conflict" ${disabled()}>重新载入…</button>
        ${view.editable ? `<button class="primary" data-action="resolve-result" data-mutates ${state.busy || stale ? "disabled" : ""}>保存并标记解决</button>` : ""}</div>
    </div>`;
}

function renderDiffPanel(): string {
  const selection = state.selection;
  if (!selection) return `<div class="empty-state">${icon(state.view === "changes" ? "changes" : "commit", 36)}<strong>${state.view === "changes" ? "选择文件查看变更" : "选择提交查看详情"}</strong></div>`;
  if (state.conflict && "path" in selection && state.conflict.path === selection.path && !state.diffLoading) return renderConflictPanel(state.conflict);
  const title = "path" in selection ? selection.path : "stash" in selection ? state.stashes.find((stash) => stash.hash === selection.stash)?.message ?? "储藏" : selection.hash.slice(0, 12);
  const subtitle = "path" in selection ? selection.staged ? "HEAD → 暂存区" : "工作区变更" : "stash" in selection ? "储藏详情" : "提交详情";
  const stashActions = "stash" in selection ? `<div class="stash-actions">
    <button data-action="apply-stash" data-mutates ${disabled()}>应用</button>
    <button data-action="pop-stash" data-mutates ${disabled()}>应用并删除</button>
    <button class="danger-text" data-action="drop-stash" data-mutates ${disabled()}>删除…</button></div>` : "";
  let body = "";
  if (state.diffLoading) body = '<div class="empty-state"><span class="spinner large"></span><span>正在读取差异…</span></div>';
  else if (state.diffError) body = `<div class="diff-error" role="alert">${escape(state.diffError)}</div>`;
  else if (!state.diff?.text) body = '<div class="empty-state"><span>没有可显示的文本差异</span></div>';
  else {
    const rows = parseDiff(state.diff.text);
    let hunk = -1;
    body = `<div class="diff-scroll"><div class="diff-code" role="region" aria-label="统一差异" tabindex="0">${rows.slice(0, 4000).map((row) => {
      if (row.kind === "hunk") hunk++;
      const action = row.kind === "hunk" && rows.length <= 4000 && !state.diff!.truncated && state.patch?.supported && state.patch.hunks[hunk]?.header === row.content && "path" in selection
        ? `<button class="hunk-action" data-action="apply-hunk" data-hunk="${state.patch.hunks[hunk]!.id}" data-mutates ${disabled()}>${selection.staged ? "取消暂存此块" : "暂存此块"}</button>` : "";
      return `<div class="diff-line ${row.kind}" ${row.kind === "hunk" ? 'tabindex="-1"' : ""}><span class="line-number" aria-hidden="true">${row.before ?? ""}</span><span class="line-number" aria-hidden="true">${row.after ?? ""}</span><code>${escape(row.content) || " "}</code>${action}</div>`;
    }).join("")}</div>
      ${state.diff.truncated || rows.length > 4000 ? '<div class="diff-limit">预览已截断，请使用命令行查看完整差异。</div>' : ""}</div>`;
  }
  const navigation = state.diff?.text ? '<button class="small-button" data-action="previous-hunk" title="上一差异块">↑</button><button class="small-button" data-action="next-hunk" title="下一差异块">↓</button>' : "";
  return `<div class="diff-heading">${icon("file", 16)}<strong title="${escape(title)}">${escape(title)}</strong><span>${subtitle}</span>${state.diff?.binary ? '<span class="badge">二进制</span>' : ""}${navigation}</div>${stashActions}${body}`;
}

async function loadSelection(): Promise<void> {
  const version = ++selectionVersion;
  const selection = state.selection;
  const repo = state.repo;
  state.diff = null;
  state.patch = null;
  state.conflict = null;
  state.diffError = "";
  state.diffLoading = !!selection;
  renderDiffOnly();
  if (!selection || !repo) return;
  try {
    if ("path" in selection && repo.files.some((file) => file.path === selection.path && file.conflict)) {
      const conflict = await api.conflict(repo.path, selection.path);
      if (version === selectionVersion) state.conflict = conflict;
    } else {
      const diff = "path" in selection ? await api.diff(repo.path, selection.path, selection.staged)
        : "stash" in selection ? await api.stashDiff(repo.path, selection.stash)
        : await api.commitDiff(repo.path, selection.hash);
      const patch = "path" in selection && !diff.binary && !diff.truncated
        ? await api.patch(repo.path, selection.path, selection.staged).catch(() => null) : null;
      if (version === selectionVersion) {
        state.patch = patch;
        state.diff = patch?.supported ? { ...diff, text: patch.text } : diff;
      }
    }
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
  if (panel) {
    panel.innerHTML = renderDiffPanel();
    if (state.stale) panel.querySelectorAll<HTMLButtonElement>("[data-mutates]").forEach((button) => { button.disabled = true; });
  }
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
  if (state.view === "stash") {
    const selection = state.selection;
    if (!selection || !("stash" in selection) || !state.stashes.some((stash) => stash.hash === selection.stash)) {
      state.selection = state.stashes[0] ? { stash: state.stashes[0].hash } : null;
    }
    return;
  }
  const groups = fileGroups(state.repo.files);
  state.checked.staged = reconcileChecked(state.checked.staged, groups.staged);
  state.checked.working = reconcileChecked(state.checked.working, groups.working);
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
  const [repo, branches, commits, stashes] = await Promise.all([api.snapshot(path), api.branches(path), api.history(path, 0), api.stashes(path)]);
  if (state.repo?.path !== path) return;
  state.repo = repo;
  state.branches = branches;
  state.commits = commits;
  state.stashes = stashes;
  state.hasMore = commits.length === 100;
  state.stale = false;
  state.library = await api.library();
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
    await adoptRepository(repo);
  });
}

async function adoptRepository(repo: Snapshot): Promise<void> {
  document.querySelector<HTMLDialogElement>("dialog[open]")?.close();
  pendingConfirm = null;
  ++selectionVersion;
  state.repo = repo;
  state.screen = "repository";
  state.selection = null;
  state.diff = null;
  state.conflict = null;
  state.query = "";
  state.checked = { staged: new Set(), working: new Set() };
  state.view = "changes";
  await refresh();
}

function showLibrary(groupID = state.libraryGroup): void {
  if (state.busy) return;
  document.querySelector<HTMLDialogElement>("dialog[open]")?.close();
  pendingConfirm = null;
  ++selectionVersion;
  state.screen = "library";
  state.libraryGroup = groupID;
  state.error = "";
  render();
}

async function updateLibrary(label: string, operation: () => Promise<Library>, success?: string): Promise<void> {
  await perform(label, async () => {
    state.library = await operation();
    if (state.libraryGroup !== "ALL" && !state.library.groups.some((group) => group.id === state.libraryGroup)) {
      state.libraryGroup = "";
    }
    if (success) setNotice(success);
  });
}

async function refreshCurrentScreen(): Promise<void> {
  if (state.screen === "library") state.library = await api.refreshLibrary();
  else await refresh();
}

const destinationGroup = () => state.libraryGroup === "ALL" ? "" : state.libraryGroup;

function groupOptions(selected: string): string {
  return `<option value="" ${!selected ? "selected" : ""}>未分组</option>${state.library.groups.map((group) =>
    `<option value="${escape(group.id)}" ${selected === group.id ? "selected" : ""}>${escape(group.name)}</option>`).join("")}`;
}

function showAddDialog(): void {
  showDialog(`<h2 id="dialog-title">添加本地仓库</h2>
    <label for="dialog-input">仓库路径</label><input id="dialog-input" name="path" required placeholder="/Users/you/Projects/repository" autocomplete="off" spellcheck="false">
    <label for="destination-group">仓库组</label><select id="destination-group" name="group">${groupOptions(destinationGroup())}</select>
    ${dialogButtons("添加")}`, "library-add-form");
}

function showGroupDialog(rename = false): void {
  const group = rename ? state.library.groups.find((group) => group.id === state.libraryGroup) : null;
  if (rename && !group) return;
  showDialog(`<h2 id="dialog-title">${rename ? "重命名仓库组" : "新建仓库组"}</h2>
    <label for="group-name">组名称</label><input id="group-name" name="name" value="${escape(group?.name ?? "")}" required maxlength="80" autocomplete="off">
    <input type="hidden" name="group-id" value="${escape(group?.id ?? "")}">
    ${dialogButtons(rename ? "保存" : "创建")}`, "group-form");
}

function showMoveDialog(path: string): void {
  const entry = state.library.repositories.find((entry) => entry.path === path);
  if (!entry) return;
  showDialog(`<h2 id="dialog-title">移动 ${escape(entry.name)}</h2>
    <input type="hidden" name="path" value="${escape(path)}">
    <label for="destination-group">仓库组</label><select id="destination-group" name="group">${groupOptions(entry.groupID)}</select>
    ${dialogButtons("移动")}`, "move-form");
}

function showScanDialog(): void {
  showDialog(`<h2 id="dialog-title">扫描文件夹</h2>
    <label for="scan-root">文件夹</label><div class="directory-input"><input id="scan-root" name="root" required placeholder="/Users/you/Projects" autocomplete="off" spellcheck="false"><button type="button" data-action="choose-scan-root">选择…</button></div>
    <label for="destination-group">加入仓库组</label><select id="destination-group" name="group">${groupOptions(destinationGroup())}</select>
    ${dialogButtons("扫描")}`, "scan-form");
}

async function scanFolder(root: string, groupID: string): Promise<void> {
  let result: ScanResult | null = null;
  await perform("正在扫描文件夹…", async () => { result = await api.scan(root); });
  if (result) showScanResults(result, groupID);
}

function showScanResults(result: ScanResult, groupID: string): void {
  const known = new Set(state.library.repositories.map((entry) => entry.path));
  showDialog(`<h2 id="dialog-title">扫描结果 <span class="muted">${result.repositories.length} 个仓库</span></h2>
    ${result.truncated ? '<p class="scan-warning">扫描达到目录或数量上限，请缩小文件夹范围后继续扫描。</p>' : ""}
    ${result.warnings.length ? `<details class="scan-warning"><summary>部分目录无法读取</summary><pre>${escape(result.warnings.join("\n"))}</pre></details>` : ""}
    <div class="scan-selection-bar"><label class="checkbox-label"><input id="scan-select-all" type="checkbox" checked>全选</label><span id="scan-selected-count" class="muted"></span></div>
    <div class="scan-results">${result.repositories.map((entry) => `<label class="scan-result">
      <input type="checkbox" name="repository" value="${escape(entry.path)}" ${known.has(entry.path) ? "disabled" : "checked"}>
      <span><strong>${escape(entry.name)}</strong><small>${escape(entry.path)}</small></span>
      <span class="muted">${known.has(entry.path) ? "已添加" : escape(entry.branch)}</span></label>`).join("") || '<div class="group-empty">未找到 Git 仓库</div>'}</div>
    <label for="destination-group">加入仓库组</label><select id="destination-group" name="group">${groupOptions(groupID)}</select>
    ${dialogButtons("添加所选仓库")}`, "scan-import-form");
  document.querySelector("#input-dialog")!.classList.add("scan-dialog");
  updateScanSelection();
}

function updateScanSelection(): void {
  const candidates = [...document.querySelectorAll<HTMLInputElement>('#scan-import-form input[name="repository"]:not(:disabled)')];
  const checked = candidates.filter((input) => input.checked).length;
  const all = document.querySelector<HTMLInputElement>("#scan-select-all")!;
  all.checked = candidates.length > 0 && checked === candidates.length;
  all.indeterminate = checked > 0 && checked < candidates.length;
  all.disabled = !candidates.length;
  document.querySelector("#scan-selected-count")!.textContent = `已选 ${checked} 个`;
  document.querySelector<HTMLButtonElement>('#scan-import-form button[type="submit"]')!.disabled = !checked;
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

function showDialog(contents: string, formID: string): void {
  if (state.busy) return;
  document.querySelector("#dialog-host")!.innerHTML = `<dialog id="input-dialog" class="input-dialog" aria-labelledby="dialog-title"><form id="${formID}">${contents}</form></dialog>`;
  document.querySelector<HTMLDialogElement>("#input-dialog")!.showModal();
  document.querySelector<HTMLElement>('#input-dialog input:not([type="hidden"]), #input-dialog select, #input-dialog button')?.focus();
}

const dialogButtons = (label: string, danger = false) => `<div class="dialog-actions"><button type="button" data-action="cancel-dialog">取消</button><button class="${danger ? "danger-button" : "primary"}" type="submit">${label}</button></div>`;

function showStashDialog(): void {
  showDialog(`<h2 id="dialog-title">储藏变更</h2>
    <label for="stash-message">说明（可选）</label><input id="stash-message" name="message" autocomplete="off">
    <label class="checkbox-label"><input type="checkbox" name="untracked" checked>包含未跟踪文件</label>
    ${dialogButtons("储藏")}`, "stash-form");
}

function showMergeDialog(): void {
  const branches = state.branches.filter((branch) => !branch.current && !branch.name.endsWith("/HEAD"));
  showDialog(`<h2 id="dialog-title">合并到 ${escape(state.repo!.branch)}</h2>
    <label for="merge-branch">选择分支</label><select id="merge-branch" name="branch" required>${branches.map((branch) => `<option value="refs/${branch.remote ? "remotes" : "heads"}/${escape(branch.name)}">${escape(branch.name)}${branch.remote ? "（远程）" : ""}</option>`).join("")}</select>
    ${dialogButtons("合并")}`, "merge-form");
}

function confirmAction(title: string, description: string, label: string, action: () => void): void {
  const target = confirmationTarget();
  pendingConfirm = () => {
    if (target !== confirmationTarget()) {
      state.error = "确认目标已改变，请重新选择后再操作";
      render();
      return;
    }
    action();
  };
  showDialog(`<h2 id="dialog-title">${escape(title)}</h2><p>${escape(description)}</p>${dialogButtons(label, true)}`, "confirm-form");
}

function confirmationTarget(): string {
  return JSON.stringify([state.screen, state.libraryGroup, state.repo?.path, state.selection, state.repo?.operation.token, state.conflict?.token]);
}

function resolveConflict(choice: string): void {
  const conflict = state.conflict;
  if (!conflict) return;
  const saved = state.conflictDrafts.get(conflictKey());
  const key = conflictKey();
  void mutate("正在解决冲突…", async (path) => {
    await api.resolveConflict(path, conflict.path, saved?.token ?? conflict.token, choice, saved?.content ?? conflict.result);
    state.conflictDrafts.delete(key);
  }, "冲突已解决并暂存");
}

function operate(action: string): void {
  const operation = state.repo?.operation;
  if (!operation?.kind) return;
  void mutate("正在更新 Git 操作…", (path) => api.operationAction(path, operation.kind, operation.token, action), action === "continue" ? "操作已继续" : action === "abort" ? "操作已中止" : "已跳过当前提交");
}

async function commit(): Promise<void> {
  if (state.screen !== "repository" || !canCommit()) return;
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
  target.closest<HTMLDetailsElement>("details.popup-menu")?.removeAttribute("open");
  if (action === "cancel-dialog") return document.querySelector<HTMLDialogElement>("dialog")?.close();
  if (action === "cancel-scan") {
    target.disabled = true;
    void api.cancelScan().catch((error) => { state.error = String(error); renderAlerts(); });
    return;
  }
  if (action === "dismiss-error") {
    state.error = "";
    renderAlerts();
    return;
  }
  if (state.busy) return;
  switch (action) {
    case "library": showLibrary(); break;
    case "library-group": showLibrary(target.dataset.group!); break;
    case "choose-local":
      void updateLibrary("正在添加本地仓库…", () => api.chooseLocal(destinationGroup()));
      break;
    case "new-group": showGroupDialog(); break;
    case "rename-group": showGroupDialog(true); break;
    case "remove-group": {
      const group = state.library.groups.find((group) => group.id === state.libraryGroup);
      if (group) confirmAction(`移除“${group.name}”？`, "组内仓库将移至未分组。", "移除组", () => {
        void updateLibrary("正在移除仓库组…", () => api.removeGroup(group.id), "仓库组已移除");
      });
      break;
    }
    case "move-repository": showMoveDialog(target.dataset.path!); break;
    case "remove-repository": {
      const path = target.dataset.path!;
      const entry = state.library.repositories.find((entry) => entry.path === path);
      if (entry) confirmAction(`移除“${entry.name}”？`, "只从本地列表移除，磁盘上的仓库文件保留。", "从列表移除", () => {
        void updateLibrary("正在移除仓库…", async () => {
          const library = await api.removeRepository(path);
          if (state.repo?.path === path) {
            state.repo = null;
            state.selection = null;
            state.conflict = null;
            state.screen = "library";
          }
          return library;
        }, "已从列表移除");
      });
      break;
    }
    case "clear-repo-search": state.libraryQuery = ""; render(); document.querySelector<HTMLInputElement>("#repo-search")?.focus(); break;
    case "scan": showScanDialog(); break;
    case "choose-scan-root": {
      const dialog = document.querySelector("dialog[open]");
      void api.chooseScanRoot().then((root) => {
        const input = document.querySelector<HTMLInputElement>("#scan-root");
        if (root && input && document.querySelector("dialog[open]") === dialog) input.value = root;
      }).catch((error) => { state.error = String(error); renderAlerts(); });
      break;
    }
    case "choose": void openRepository(); break;
    case "open-path": showAddDialog(); break;
    case "open": void openRepository(target.dataset.path); break;
    case "new-branch": showInputDialog("branch"); break;
    case "refresh": void perform("正在刷新…", refreshCurrentScreen); break;
    case "changes":
    case "history":
    case "stash":
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
    case "select-stash":
      state.selection = { stash: target.dataset.hash! };
      render();
      void loadSelection();
      break;
    case "new-stash": showStashDialog(); break;
    case "merge": showMergeDialog(); break;
    case "apply-stash":
    case "pop-stash": {
      const selection = state.selection;
      if (!selection || !("stash" in selection)) break;
      void mutate("正在应用储藏…", (path) => api.applyStash(path, selection.stash, action === "pop-stash"), "储藏已应用");
      break;
    }
    case "drop-stash": {
      const selection = state.selection;
      if (!selection || !("stash" in selection)) break;
      confirmAction("删除储藏？", "这会删除所选储藏记录，无法在应用内撤销。", "删除储藏", () => {
        void mutate("正在删除储藏…", (path) => api.dropStash(path, selection.stash), "储藏已删除");
      });
      break;
    }
    case "resolve-ours": resolveConflict("ours"); break;
    case "resolve-theirs": resolveConflict("theirs"); break;
    case "resolve-result": resolveConflict("result"); break;
    case "resolve-delete":
      confirmAction("删除冲突文件？", "文件将从工作区删除，并暂存此解决结果。", "删除文件", () => resolveConflict("delete"));
      break;
    case "reload-conflict":
      confirmAction("重新载入冲突？", "当前合并结果草稿将被磁盘上的版本替换。", "重新载入", () => {
        state.conflictDrafts.delete(conflictKey());
        void loadSelection();
      });
      break;
    case "operation-continue": operate("continue"); break;
    case "operation-abort":
      confirmAction("中止当前操作？", "Git 将恢复操作开始前的状态，当前冲突解决结果可能丢失。", "中止操作", () => operate("abort"));
      break;
    case "operation-skip":
      confirmAction("跳过当前提交？", "当前提交及其冲突解决结果将不再应用。", "跳过提交", () => operate("skip"));
      break;
    case "stage":
    case "unstage": {
      const method = action === "stage" ? api.stage : api.unstage;
      void mutate(action === "stage" ? "正在暂存…" : "正在取消暂存…", (path) => method(path, [target.dataset.path!]));
      break;
    }
    case "apply-hunk": {
      const selection = state.selection;
      const patch = state.patch;
      if (!selection || !("path" in selection) || !patch?.supported || state.diffLoading) break;
      const id = Number(target.dataset.hunk);
      if (!patch.hunks.some((hunk) => hunk.id === id)) break;
      void mutate(selection.staged ? "正在取消暂存区块…" : "正在暂存区块…",
        (path) => api.applyHunk(path, selection.path, selection.staged, patch.token, id));
      break;
    }
    case "previous-hunk":
    case "next-hunk": {
      const hunks = [...document.querySelectorAll<HTMLElement>(".diff-line.hunk")];
      const top = document.querySelector(".diff-scroll")?.getBoundingClientRect().top ?? 0;
      const candidate = action === "next-hunk" ? hunks.find((hunk) => hunk.getBoundingClientRect().top > top + 5)
        : hunks.reverse().find((hunk) => hunk.getBoundingClientRect().top < top - 5);
      candidate?.scrollIntoView({ block: "start" });
      candidate?.focus({ preventScroll: true });
      break;
    }
    case "stage-all":
    case "unstage-all": {
      if (!state.repo) break;
      const groups = fileGroups(state.repo.files);
      const files = visibleFiles(action === "stage-all" ? groups.working : groups.staged, state.query, state.fileSort);
      const checked = checkedPaths(files, action === "stage-all" ? state.checked.working : state.checked.staged);
      const paths = checked.length ? checked : files.filter((file) => !file.conflict).map((file) => file.path);
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
  if (target.id === "conflict-result" && state.conflict) {
    const previous = state.conflictDrafts.get(conflictKey());
    state.conflictDrafts.set(conflictKey(), { token: previous?.token ?? state.conflict.token, content: target.value });
  }
  if (target.id === "repo-search") {
    state.libraryQuery = target.value;
    render();
  }
  if (target.id === "filter") {
    state.query = target.value;
    render();
  }
});

document.addEventListener("change", (event) => {
  const target = event.target as HTMLInputElement;
  if (target.id === "file-sort") {
    state.fileSort = target.value === "name" ? "name" : "path";
    render();
  } else if (target.matches(".group-check, .file-check") && state.repo && !state.busy && !state.stale) {
    const staged = target.dataset.staged === "true";
    const checked = staged ? state.checked.staged : state.checked.working;
    const files = visibleFiles(staged ? fileGroups(state.repo.files).staged : fileGroups(state.repo.files).working, state.query, state.fileSort);
    for (const file of files) {
      if (file.conflict || (target.matches(".file-check") && file.path !== target.dataset.path)) continue;
      if (target.checked) checked.add(file.path);
      else checked.delete(file.path);
    }
    updateFileChecks();
  } else if (target.id === "scan-select-all") {
    document.querySelectorAll<HTMLInputElement>('#scan-import-form input[name="repository"]:not(:disabled)').forEach((input) => { input.checked = target.checked; });
    updateScanSelection();
  } else if (target.name === "repository" && target.closest("#scan-import-form")) {
    updateScanSelection();
  }
});

function resizePanel(kind: string, value: number): void {
  const content = document.querySelector<HTMLElement>(".changes-content");
  if (!content) return;
  if (kind === "width") {
    layout.width = Math.max(260, Math.min(Math.max(260, content.clientWidth - 305), value, 700));
    content.style.setProperty("--files-width", `${layout.width}px`);
  } else {
    layout.split = Math.max(15, Math.min(85, value));
    content.style.setProperty("--files-split", `${layout.split}fr`);
    content.style.setProperty("--working-split", `${100 - layout.split}fr`);
  }
}

function saveLayout(): void {
  try {
    localStorage.setItem("files-width", String(layout.width));
    localStorage.setItem("files-split", String(layout.split));
  } catch {}
}

document.addEventListener("pointerdown", (event) => {
  const divider = (event.target as HTMLElement).closest<HTMLElement>("[data-resize]");
  if (!divider || event.button !== 0) return;
  event.preventDefault();
  const kind = divider.dataset.resize!;
  const bounds = document.querySelector(kind === "width" ? ".changes-content" : ".file-groups")!.getBoundingClientRect();
  divider.setPointerCapture(event.pointerId);
  const move = (pointer: PointerEvent) => resizePanel(kind, kind === "width" ? pointer.clientX - bounds.left : (pointer.clientY - bounds.top) / bounds.height * 100);
  const stop = () => {
    divider.removeEventListener("pointermove", move);
    divider.removeEventListener("lostpointercapture", stop);
    saveLayout();
  };
  divider.addEventListener("pointermove", move);
  divider.addEventListener("lostpointercapture", stop);
});

document.addEventListener("keydown", (event) => {
  const divider = (event.target as HTMLElement).closest<HTMLElement>("[data-resize]");
  if (!divider || !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) return;
  event.preventDefault();
  const kind = divider.dataset.resize!;
  const direction = event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 1;
  const width = document.querySelector(".files-panel")!.getBoundingClientRect().width;
  resizePanel(kind, kind === "width" ? width + direction * 20 : layout.split + direction * 5);
  saveLayout();
});

document.addEventListener("submit", (event) => {
  const form = event.target as HTMLFormElement;
  event.preventDefault();
  if (form.id === "commit-form") void commit();
  const data = new FormData(form);
  if (form.id === "library-add-form") {
    const path = String(data.get("path") ?? "").trim();
    const group = String(data.get("group") ?? "");
    if (!path || state.busy) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void updateLibrary("正在添加本地仓库…", () => api.addRepositories([path], group), "已添加本地仓库");
  }
  if (form.id === "group-form") {
    const id = String(data.get("group-id") ?? "");
    const name = String(data.get("name") ?? "").trim();
    if (!name || state.busy) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void updateLibrary("正在保存仓库组…", async () => {
      const library = id ? await api.renameGroup(id, name) : await api.createGroup(name);
      if (!id) state.libraryGroup = library.groups.find((group) => group.name === name)!.id;
      return library;
    });
  }
  if (form.id === "move-form") {
    const path = String(data.get("path") ?? "");
    const group = String(data.get("group") ?? "");
    if (state.busy) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void updateLibrary("正在移动仓库…", () => api.moveRepository(path, group));
  }
  if (form.id === "scan-form") {
    const root = String(data.get("root") ?? "").trim();
    const group = String(data.get("group") ?? "");
    if (!root || state.busy) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void scanFolder(root, group);
  }
  if (form.id === "scan-import-form") {
    const paths = data.getAll("repository").map(String);
    const group = String(data.get("group") ?? "");
    if (!paths.length || state.busy) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void updateLibrary("正在添加本地仓库…", () => api.addRepositories(paths, group), `已添加 ${paths.length} 个仓库`);
  }
  if (form.id === "stash-form") {
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void mutate("正在储藏变更…", (path) => api.createStash(path, String(data.get("message") ?? ""), data.get("untracked") === "on"), "变更已储藏");
  }
  if (form.id === "merge-form") {
    const branch = String(data.get("branch") ?? "");
    if (!branch) return;
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    void mutate("正在合并分支…", (path) => api.merge(path, branch), "分支已合并");
  }
  if (form.id === "confirm-form") {
    document.querySelector<HTMLDialogElement>("dialog")!.close();
    const action = pendingConfirm;
    pendingConfirm = null;
    action?.();
  }
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
  if (document.querySelector("dialog[open]")) return;
  if (event.key.toLowerCase() === "f" && state.screen === "library") {
    event.preventDefault();
    document.querySelector<HTMLInputElement>("#repo-search")?.focus();
    return;
  }
  if (event.key === "Enter") {
    event.preventDefault();
    void commit();
  }
});

window.addEventListener("focus", () => {
  if (state.screen === "repository" && state.repo && !state.busy && !document.querySelector("dialog[open]") && document.activeElement?.id !== "conflict-result") {
    void refresh().catch((error) => {
      state.error = error instanceof Error ? error.message : String(error);
      render();
    });
  }
});

events.repositoryAdd.on(() => {
  if (state.busy) return;
  showLibrary();
  void updateLibrary("正在添加本地仓库…", () => api.chooseLocal(destinationGroup()));
});
events.repositoryLibrary.on(() => { showLibrary(); });
events.repositoryScan.on(() => { if (!state.busy) { showLibrary(); showScanDialog(); } });
events.repositoryGroup.on(() => { if (!state.busy) { showLibrary(); showGroupDialog(); } });
events.repositoryRefresh.on(() => { void perform("正在刷新…", refreshCurrentScreen); });
window.addEventListener("beforeunload", () => clearTimeout(noticeTimer));

render();
void api.library().then((library) => {
  state.library = library;
  render();
}).catch((error) => {
  state.error = error instanceof Error ? error.message : String(error);
  renderAlerts();
});
