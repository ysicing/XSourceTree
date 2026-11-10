# Repository Guidelines

## 项目结构

XSourceTree 是使用 Go 和 MyGo Native UI 构建的 macOS Git 客户端，调用系统 Git。

- 根目录 Go 文件：应用生命周期、原生界面、弹窗及服务。
- `internal/gitrepo/`：Git 命令执行、解析与仓库操作。
- `internal/workspace/`：仓库发现、分组与已打开仓库管理。
- 测试与源码相邻：Go 使用 `*_test.go`，脚本使用 `scripts/*.test.mjs`。
- `resources/`、`assets/`：应用图标、品牌及文件图标。
- `docs/development.md`、`docs/usage.md`：开发发布与使用说明。

## 开发与验证

需要 Go 1.27.3+、Bun、Git、Task 和 macOS Xcode Command Line Tools。

- `task setup`：安装打包工具与 Go 依赖。
- `task dev`：启动原生桌面开发模式。
- `task build`：生成 ad-hoc 签名应用及 DMG，输出至 `build/darwin-*`。
- `task fmt`：补齐版权头并格式化 Go；注意检查仓库范围的改动。
- `task fmt:check`：只读检查文件头与格式。
- `task check`：串行执行格式检查、测试与 lint。

重任务串行运行，保留 Taskfile 的并发限制：`GOMAXPROCS=2`、`-p=1`，测试另加 `-parallel=2`。

## 代码风格

Go 使用制表符缩进，导出标识符用 `PascalCase`，非导出用 `camelCase`，文件名使用有意义的下划线命名。MJS 沿用两空格缩进。

Go 格式化使用 `gofmt`、`goimports` 和 `gci`；检查规则见 `.golangci.yml`，包含 `goheader` 与 `govet`。新源码沿用现有 AGPL 文件头，Markdown 无需添加。

## 界面组件

- 优先复用 [MyGo Native UI 原生组件](https://mygo.egoist.dev/docs/ui#actions)，包括按钮、菜单、分段控件、工具栏、输入框及弹窗；现有组件无法满足需求时再自定义。
- 使用仓库依赖版本支持的 API，沿用组件的焦点、键盘操作、禁用状态及主题行为，避免重复实现。

## 测试规范

使用 Go `testing`、MyGo `ui.Tester` 和 Bun 测试。Go 用例命名为 `TestBehavior`；Git 测试仅操作临时仓库及本地 bare 远程。

按影响范围运行 `task test:go`、`task test:ui` 或 `task test:release`；`task test` 依次运行 Go 与脚本测试。未设置覆盖率阈值，应覆盖行为变化、失败路径与数据保护。原生自动化流程见 `docs/development.md`。

## 提交与 PR

沿用 `<type>(scope): <summary>`，摘要用简短中文动作描述，例如 `feat(ui): 优化仓库导航与提交历史体验`。

改动保持聚焦。PR 说明行为变化、验证结果及兼容性风险，按需关联 issue；界面变化附截图。

## 架构与安全

Git 操作放在后台，通过 `Window.Update` 更新界面；命令使用参数数组，并支持取消与超时。保留丢弃修改时的恢复副本。凭证及签名私钥不得入库，发布须明确授权。
