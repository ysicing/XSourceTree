# 开发与发布

[返回首页](../README.md) · [使用说明](usage.md)

## 本地开发

需要 Go **1.27.3+**、Bun、Git 和 Task；macOS 需要 Xcode Command Line Tools。所有任务定义在 [Taskfile.yml](../Taskfile.yml)，运行 `task` 查看列表。

```sh
task setup
task dev
```

修改 Go 服务签名后生成类型：

```sh
task generate
```

构建 macOS 应用与 DMG：

```sh
task build
```

产物位于 `build/darwin-arm64/`（Intel 机器为 `darwin-amd64`）。默认使用本地 ad-hoc 签名，Developer ID 签名与公证尚未配置。应用更新包另用 Ed25519 签名校验，不能替代 macOS 的代码签名与公证。

应用图标源文件为 `resources/icon.png`，MyGo 构建时自动使用。透明底 Logo 位于 `assets/brand/logo.png`，macOS 图标文件位于 `assets/brand/XSourceTree.icns`。

## 验证

`task check` 串行执行全部测试、类型检查与前端构建。所有 Git 集成测试只操作测试创建的临时仓库，包括本地 bare 远程仓库，不连接真实远程服务。

```sh
task check
```

可用 `task test:go`、`task test:web`、`task test:release`、`task typecheck` 或 `task build:web` 单独执行检查，`task test` 依次执行三组测试。Go 测试使用 `GOMAXPROCS=2`、`-p=1` 和 `-parallel=2`；开发、生成绑定与构建任务同样限制 Go 构建并发。

真实桌面端集成测试通过 MyGo 的 automation 插件操作应用。插件仅编译进 `e2e` 构建，并且仅在设置 socket 路径时启用。先启动测试用应用，再在另一个终端执行测试：

```sh
GOMAXPROCS=2 GOFLAGS="-p=1 -tags=e2e" \
  MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock bun run dev
```

```sh
MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock node scripts/smoke.mjs
```

测试会在临时目录创建仓库，验证本地添加、扫描勾选、搜索、分组和持久化，以及暂存、提交、逐块/逐行操作、丢弃确认与外部编辑保护、并排布局及持久化、储藏和冲突解决；覆盖超过 100 条历史的搜索与翻页、提交单文件差异、大提交差异失败后的文件切换、分支重命名与删除保护、标签创建与删除，并验证 hook 失败后的状态同步和刷新失败后的写操作保护。启用自动化时，测试应用使用独立的临时数据目录，退出时清理，不影响普通开发版或正式版的本地仓库列表及恢复副本。

远程流程使用独立脚本，在重新启动的测试应用中运行，验证远程配置、首次发布及上游、普通推送、远程分支检出，以及确认后目标地址或提交变化时的保护。推送目标全部为临时本地 bare 仓库：

```sh
MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock node scripts/remotes-smoke.mjs
```

## 项目结构

以下路径均相对于仓库根目录：

```text
internal/gitrepo/     系统 Git 调用、状态解析与仓库操作，无桌面依赖
internal/workspace/   本地仓库库、扫描、分组与已打开仓库管理，无桌面依赖
main.go              MyGo 窗口与原生菜单
service.go           Go ↔ TypeScript 服务边界
src/                 TypeScript 界面、Diff 解析与提交图
scripts/smoke.mjs     真实桌面端集成测试
```

Git 命令使用参数数组执行，路径按字面值处理。写操作限定在已打开的仓库内，文件变更操作校验当前状态；取消暂存保留工作区内容。命令设有超时，Diff 禁用外部 diff/textconv 程序，输出和预览大小设有限制。窗口关闭会取消该窗口发起的 Git 请求。

仓库现有的 Git 配置、hooks 和签名配置仍然生效。打开仓库应与直接在该仓库运行 Git 一样，信任其配置与 hooks。

## 应用更新与发布

以下命令均在仓库根目录执行。

### 存储与签名

更新包与清单放在自有对象存储，沿用 XStats 的 `mc` 存储别名和 CDN，使用独立目录。[release.config.json](../release.config.json) 统一维护下载根地址 `https://c.ysicing.net/oss/apps/macOS/XSourceTree`、上传目标 `c-ip/oss/apps/macOS/XSourceTree` 和签名公钥。客户端直接通过 HTTPS 检查与下载，再交由 MyGo 验签安装。存储凭证仅由发布机器的 `mc` 管理。

Apple Silicon 和 Intel 分别读取下载根地址下的 `update-darwin-arm64.json` 和 `update-darwin-amd64.json`；当前发布完整更新包，不生成增量包。发布脚本要求两个平台的清单与归档齐全，且版本为 `x.y.z` 稳定版。

版本统一修改 [package.json](../package.json) 的 `version`，MyGo 构建配置、关于窗口与页面版本号均使用该版本；同时在 [CHANGELOG.md](../CHANGELOG.md) 添加对应 `## <version>` 节，构建时写入更新说明。

发布签名私钥必须与 `release.config.json` 中的公钥配对；私钥保存在仓库外并离线备份，不要重新生成替换，否则已安装应用将拒绝新签名。当前维护者本机路径为 `~/.config/xsourcetree/update-keys/mygo-update.key`；其他构建环境通过 `MYGO_UPDATER_PRIVATE_KEY` 提供同一私钥。

### 构建与上传

本地构建签名更新包（不上传）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  task build
```

没有私钥的普通构建仍可检查和安装由维护者签名的更新，但不生成可发布的更新清单。发布机器需安装 `mc` 并配置 `c-ip` 别名。完成版本修改与验证后，可先构建双架构并仅在本地校验：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  task release -- --dry-run
```

该命令使用全新的 `build/release-*` 目录，避免复用旧产物，并输出本次目录。确认后发布这份已验证的产物，无需重新构建：

```sh
task publish -- --dir build/release-实际目录 --dry-run
task publish -- --dir build/release-实际目录
```

也可直接构建并发布：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  task release
```

### 发布校验与失败恢复

发布脚本先验证两个架构的版本、URL、大小与 Ed25519 签名，再上传带版本号的 DMG、归档及 `<version>/update-darwin-*.json` 历史清单。版本对象通过条件 PUT 禁止覆盖；已存在且内容相同可重跑，内容不同必须提升版本。全部版本对象经 CDN 完整回读并验证 SHA-256 后，才更新两份固定更新清单并回读确认；固定清单设置 `Cache-Control: no-cache,max-age=0,must-revalidate`，CDN 需遵守该响应头，不能强制长缓存。

同一产品发布须串行执行，两份架构清单并非原子更新；中途失败可使用同一产物目录重跑，脚本拒绝覆盖新版本清单为旧版本。脚本不会修改存储权限、发布 GitHub Release、创建标签或提交推送 Git。
