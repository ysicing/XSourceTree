# 文件类型图标

此目录的 SVG 原样摘自 [Material Icon Theme](https://github.com/material-extensions/vscode-material-icon-theme)，版本固定为提交 `ced21b9b5241c1efa502a8c46ec7363a690a7058`，上游路径为 `icons/`。

SVG 按 [MIT 许可证](LICENSE) 使用，版权归 Material Extensions 所有。仅内置文件列表使用的图标，不依赖编辑器插件或网络请求。

文件识别与原生渲染统一位于 `internal/app/file_icons.go`，本目录的 `icons.go` 嵌入图标和 MIT 许可证，供应用包读取。图标在启动时解析一次；使用 MyGo `ui.Image` 保留原有颜色，浅色主题优先使用上游提供的 `_light` 版本，未知文件使用随主题变化的通用文件图标。
