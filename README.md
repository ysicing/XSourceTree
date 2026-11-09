# XSourceTree

基于 [MyGo](https://github.com/egoist/mygo) 的开源 Git 桌面客户端。优先支持 macOS，Git 核心使用跨平台 Go 实现。

## 已实现

- 本地仓库库：原生多选或输入路径添加已有仓库，自动去重，不限制为最近 12 个
- 递归扫描文件夹，勾选后批量加入；支持普通工作区和 Git worktree
- 仓库组创建、重命名、移除，以及仓库在组间移动；从列表移除不删除磁盘文件
- 按名称、路径、分支和组名搜索，与仓库组筛选组合使用
- 文件状态、未跟踪文件预览、工作区与暂存区 Diff
- 上下独立滚动的已暂存/未暂存文件区、右侧 Diff 和底部提交栏，支持拖动或方向键调整面板并记住尺寸
- 按路径或名称排序、筛选文件、勾选与分区全选/半选，按所选匹配文件批量暂存/取消暂存、提交与提交草稿
- 所有分支的提交历史、提交关系图、提交详情与分页
- 本地分支创建和切换、远程 fetch/pull/push、上游领先/落后计数
- 储藏列表与 Diff、创建储藏（可包含未跟踪文件）、应用、应用并删除及确认删除
- 合并本地或远程分支、冲突基础/当前/传入版本预览、合并结果编辑并暂存
- 文本、二进制及修改/删除冲突的版本选择；识别 merge/rebase/cherry-pick/revert，继续、中止或跳过提交
- 浅色/深色外观、原生菜单、⌘O 添加、⌘⇧O 扫描、⌘⇧L 本地仓库、⌘R 刷新、⌘Enter 提交

仓库管理仅收录本机已有仓库，不提供远端仓库浏览或克隆入口；已打开仓库仍支持 fetch/pull/push。旧版本的最近仓库记录自动保留，仓库组及本地列表持久化存储。列表中的分支和变更数来自最近一次读取，可在本地仓库界面刷新；无法读取的仓库保留在列表中并标明状态。

扫描不跟随子目录符号链接，跳过隐藏目录、`node_modules` 和 `vendor`，最多扫描 12 层、20,000 个目录或 2,000 个仓库，并支持取消。达到上限或遇到无法读取的目录时会提示。扫描本身只读取，只有勾选并添加后才保存到本地列表；加入列表不会自动打开仓库。

尚未实现逐块暂存、发起交互式 rebase、标签管理、worktree 管理和凭证配置界面。远程操作使用现有 Git/SSH 凭证；pull 只允许快进，出现分叉时可先获取，再合并对应远程分支。push 需要已配置上游。当前版本不会自动填写或交互式请求密码。

冲突编辑支持最大 1 MiB 的 UTF-8 文本；二进制、大文件及符号链接可以选择完整版本或删除。子模块冲突需要外部 Git 工具处理。应用储藏发生冲突时保留储藏记录，解决后由用户决定是否删除。冲突保存会核对读取时的文件与索引版本，防止覆盖已经发生的外部编辑；普通暂存不会直接将冲突标记为解决。Git 操作仍会使用仓库配置的 hooks 和提交签名，应用内继续操作不会打开终端编辑器。

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

测试会在临时目录创建仓库，验证本地添加、扫描勾选、搜索、分组和持久化，以及暂存、提交、分支、储藏和冲突解决操作，并验证 hook 失败后的状态同步和刷新失败后的写操作保护。启用自动化时，测试应用使用独立的临时数据目录，退出时清理，不影响普通开发版或正式版的本地仓库列表。

## 结构

```text
internal/gitrepo/     系统 Git 调用、状态解析与仓库操作，无桌面依赖
internal/workspace/   本地仓库库、扫描、分组与已打开仓库管理，无桌面依赖
main.go              MyGo 窗口与原生菜单
service.go           Go ↔ TypeScript 服务边界
src/                 TypeScript 界面、Diff 解析与提交图
scripts/smoke.mjs    真实桌面端集成测试
```

Git 命令使用参数数组执行，路径按字面值处理。写操作限定在已打开的仓库内，文件变更操作校验当前状态；取消暂存保留工作区内容。命令设有超时，Diff 禁用外部 diff/textconv 程序，输出和预览大小设有限制。窗口关闭会取消该窗口发起的 Git 请求。

仓库现有的 Git 配置、hooks 和签名配置仍然生效。打开仓库应与直接在该仓库运行 Git 一样，信任其配置与 hooks。

## License

MIT
