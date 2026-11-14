/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { defineConfig } from "mygo-cli";
import { version } from "./package.json";
import release from "./release.config.json";

// 跨编译入口显式设置目标 OS；保留 macOS 原有本地构建的默认行为。
const targetOS = process.env.MYGO_TARGET_OS || (process.platform === "win32" ? "windows" : process.platform);
if (!["darwin", "windows", "linux"].includes(targetOS)) {
  throw new Error(`不支持的 MYGO_TARGET_OS: ${targetOS}`);
}

export default defineConfig({
  name: "51Code",
  slug: "51code",
  identifier: "com.ysicing.xsourcetree",
  version,
  out: "build",
  i18n: { dir: "locales", source: "zh-Hans" },
  macos: {
    minimumSystemVersion: "12.0",
    ...(process.env.XSOURCETREE_BUILD_NUMBER ? {
      infoPlist: {
        CFBundleVersion: process.env.XSOURCETREE_BUILD_NUMBER,
        XSourceTreeCommit: process.env.XSOURCETREE_BUILD_COMMIT,
        XSourceTreeBuildTime: process.env.XSOURCETREE_BUILD_TIME,
      },
    } : {}),
    ...(process.env.XSOURCETREE_RELEASE === "1" ? {
      signingIdentity: release.macos.signingIdentity,
      notarize: { keychainProfile: release.macos.notaryProfile },
    } : {}),
  },
  // 原生界面无需 WebKitGTK；Git 和 Secret Service 是 Linux 运行时依赖。
  linux: {
    formats: ["deb"],
    webview: false,
    command: "51code",
    categories: ["Development"],
    depends: ["git", "libsecret-1-0"],
  },
  // 新平台首版通过手动下载安装包升级，避免嵌入尚未发布的更新源。
  // Windows 未配置证书或 signCommand，首版安装包不提供 Authenticode 签名。
  ...(targetOS === "darwin" ? { updates: {
    publicKey: release.publicKey,
    url: release.downloadBase,
    deltas: 0,
  } } : {}),
});
