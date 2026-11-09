# XSourceTree

基于 [MyGo](https://github.com/egoist/mygo) 的开源 Git 桌面客户端。优先支持 macOS，Git 核心使用跨平台 Go 实现。

## 已实现

- 原生目录选择、路径打开、最近仓库记录
- 文件状态、未跟踪文件预览、工作区与暂存区 Diff
- 按文件或批量暂存/取消暂存、提交与提交草稿
- 所有分支的提交历史、提交关系图、提交详情与分页
- 本地分支创建和切换、远程 fetch/pull/push、上游领先/落后计数
- 浅色/深色外观、原生菜单、⌘O 打开、⌘R 刷新、⌘Enter 提交

尚未实现 clone、逐块暂存、可视化冲突编辑、交互式 rebase、stash、标签管理、worktree 管理和凭证配置界面。远程操作使用现有 Git/SSH 凭证；pull 只允许快进，push 需要已配置上游。当前版本不会自动填写或交互式请求密码。

## 开发

需要 Go **1.27.3+**、Bun、Git；macOS 需要 Xcode Command Line Tools。

```sh
bun install
go mod download
bun run dev
```

修改 Go 服务签名后生成类型：

```sh
bun run generate
```

构建 macOS 应用与 DMG：

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 bun run build
```

产物位于 `build/darwin-arm64/`（Intel 机器为 `darwin-amd64`）。默认使用本地 ad-hoc 签名，正式发布所需的 Developer ID 签名、公证与发布流程尚未配置。

## 验证

所有 Git 集成测试只操作测试创建的临时仓库，包括本地 bare 远程仓库，不连接真实远程服务。

```sh
GOMAXPROCS=2 go test -p=1 -parallel=2 ./...
bun run test:web
bun run typecheck
bun run build:web
```

真实桌面端集成测试通过 MyGo 的 automation 插件操作应用。插件仅编译进 `e2e` 构建，并且仅在设置 socket 路径时启用。先启动测试用应用，再在另一个终端执行测试：

```sh
GOMAXPROCS=2 GOFLAGS="-p=1 -tags=e2e" \
  MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock bun run dev
```

```sh
MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock node scripts/smoke.mjs
```

测试会在临时目录创建仓库，进行真实暂存、提交与分支操作，并验证 hook 失败后的状态同步和刷新失败后的写操作保护。启用自动化时，测试应用使用独立的临时数据目录，退出时清理，不影响普通开发版或正式版的最近仓库记录。

## 结构

```text
internal/gitrepo/     系统 Git 调用、状态解析与仓库操作，无桌面依赖
internal/workspace/   已打开仓库与最近仓库持久化，无桌面依赖
main.go              MyGo 窗口与原生菜单
service.go           Go ↔ TypeScript 服务边界
src/                 TypeScript 界面、Diff 解析与提交图
scripts/smoke.mjs    真实桌面端集成测试
```

Git 命令使用参数数组执行，路径按字面值处理。写操作限定在已打开的仓库内，文件变更操作校验当前状态；取消暂存保留工作区内容。命令设有超时，Diff 禁用外部 diff/textconv 程序，输出和预览大小设有限制。窗口关闭会取消该窗口发起的 Git 请求。

仓库现有的 Git 配置、hooks 和签名配置仍然生效。打开仓库应与直接在该仓库运行 Git 一样，信任其配置与 hooks。

## License

MIT
