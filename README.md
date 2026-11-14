<p align="center">
  <img src="resources/icon.png" width="128" height="128" alt="XSourceTree 应用图标">
</p>

# XSourceTree

轻量的开源 Git 桌面客户端，专注 SourceTree 中常用的工作流。优先支持 macOS，使用 Go 和 [MyGo Native UI](https://mygo.egoist.dev/docs/ui) 构建界面，使用系统 Git。

## 功能

- **仓库管理**：添加、扫描本地仓库，支持分组与搜索
- **查看与提交**：文件筛选、批量暂存、逐块/逐行暂存、提交草稿、自适应统一/并排 Diff、只读全文与文件内查找
- **历史记录**：提交关系图、全文搜索、文件路径筛选、提交差异与任意两版本比较
- **分支与标签**：管理本地分支和标签、检出远程分支、首次发布与上游跟踪
- **远程同步**：管理远程地址、Fetch、快进 Pull、Push 与领先/落后计数
- **储藏与合并**：管理储藏、合并分支、编辑冲突，继续或中止进行中的 Git 操作
- **Worktree**：发现、打开、创建工作目录，合并到当前工作区并核对状态后移除已合并目录
- **桌面体验**：浅色/深色外观、原生菜单、快捷键、可调整面板与应用内签名更新

目前管理本机已有仓库，尚无克隆入口、交互式 rebase 或凭证配置界面。远程操作沿用系统 Git/SSH 凭证。丢弃文件修改前会保存恢复副本，详见[使用说明](docs/usage.md)。

## 本地运行

需要 Go **1.27.3+**、Bun、Git 和 Task；macOS 另需 Xcode Command Line Tools。

```sh
task setup
task dev
```

构建 macOS 应用与 DMG：

```sh
task build
```

产物位于 `build/darwin-arm64/` 或 `build/darwin-amd64/`。本地构建使用 ad-hoc 签名；正式发布流程使用 Developer ID 签名、Apple 公证及 Gatekeeper 校验，详见[开发与发布](docs/development.md)。

运行 `task` 查看全部任务，`task fmt` 补齐版权头并格式化 Go 代码，`task check` 串行执行文件头与格式检查、Go 测试、原生界面测试、发布脚本测试及 golangci-lint。Bun 仅用于打包工具和发布脚本，界面无需 WebView 或前端构建。

## 文档

- [使用说明](docs/usage.md)：快捷键、更新、恢复副本与功能边界
- [开发与发布](docs/development.md)：原生界面开发、测试、项目结构与对象存储发布流程
- [更新日志](CHANGELOG.md)

## License

Copyright (c) 2026 ysicing

本项目采用 [GNU AGPL v3.0](LICENSE)（`AGPL-3.0-only`）授权。
