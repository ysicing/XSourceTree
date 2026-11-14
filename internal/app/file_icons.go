// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"path"
	"strings"

	fileicons "github.com/ysicing/51code/assets/file-icons"

	"github.com/egoist/mygo/ui"
)

var nativeFileIcons = loadNativeFileIcons()

func loadNativeFileIcons() map[string]*ui.SVG {
	entries, err := fileicons.Files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	icons := make(map[string]*ui.SVG, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".svg") {
			continue
		}
		data, err := fileicons.Files.ReadFile(entry.Name())
		if err != nil {
			panic(err)
		}
		icons[strings.TrimSuffix(entry.Name(), ".svg")] = ui.MustParseSVG(data)
	}
	return icons
}

func nativeFileIconName(filename string) string {
	name := strings.ToLower(path.Base(filename))
	switch name {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return "go"
	case ".gitignore", ".gitattributes", ".gitmodules", ".gitkeep":
		return "git"
	case "dockerfile", "containerfile", ".dockerignore":
		return "docker"
	case "makefile", "gnumakefile", ".bashrc", ".zshrc", ".bash_profile", ".zprofile":
		return "console"
	case ".env", ".editorconfig":
		return "settings"
	}
	switch {
	case strings.HasPrefix(name, "dockerfile."), strings.HasPrefix(name, "containerfile."):
		return "docker"
	case strings.HasPrefix(name, ".env."):
		return "settings"
	}
	switch path.Ext(name) {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".jsx", ".tsx":
		return "react"
	case ".py", ".pyw", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".swift":
		return "swift"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hxx", ".hh":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb", ".rake", ".gemspec":
		return "ruby"
	case ".php":
		return "php"
	case ".vue":
		return "vue"
	case ".html", ".htm":
		return "html"
	case ".css", ".scss", ".sass", ".less":
		return "css"
	case ".json", ".jsonc", ".json5":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md", ".markdown", ".mdx":
		return "markdown"
	case ".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd":
		return "console"
	case ".ini", ".conf", ".cfg", ".properties":
		return "settings"
	case ".dockerfile":
		return "docker"
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".icns", ".avif":
		return "image"
	case ".zip", ".gz", ".tgz", ".tar", ".bz2", ".xz", ".7z", ".rar":
		return "zip"
	default:
		return ""
	}
}

func nativeFileIcon(c *ui.Context, filename string) {
	name := nativeFileIconName(filename)
	icon := nativeFileIcons[name]
	if light := nativeFileIcons[name+"_light"]; !c.Theme().Dark && light != nil {
		icon = light
	}
	if icon != nil {
		ui.Image(c, icon).Size(16, 16).Shrink(0).AccessHidden(true)
		return
	}
	ui.Icon(c, nativeIcons["file"]).Size(16, 16).Shrink(0).TextColor(c.Theme().TextMuted)
}
