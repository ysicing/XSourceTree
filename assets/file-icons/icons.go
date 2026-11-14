// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only

package fileicons

import "embed"

// Files 嵌入原始文件图标与许可证，供应用包在迁移后继续读取。
//
//go:embed *.svg LICENSE
var Files embed.FS
