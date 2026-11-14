/**
 * Copyright 2026 ysicing
 * SPDX-License-Identifier: AGPL-3.0-only
 */

import { defineConfig } from "mygo-cli";
import { version } from "./package.json";
import release from "./release.config.json";

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
  updates: {
    publicKey: release.publicKey,
    url: release.downloadBase,
    deltas: 0,
  },
});
