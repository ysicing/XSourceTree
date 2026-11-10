import { defineConfig } from "mygo-cli";
import { version } from "./package.json";
import release from "./release.config.json";

export default defineConfig({
  name: "XSourceTree",
  slug: "xsourcetree",
  identifier: "com.ysicing.xsourcetree",
  version,
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  macos: { minimumSystemVersion: "12.0" },
  updates: {
    publicKey: release.publicKey,
    url: release.downloadBase,
    deltas: 0,
  },
});
