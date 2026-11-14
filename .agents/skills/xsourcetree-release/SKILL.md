---
name: xsourcetree-release
description: 为 51Code（原 XSourceTree）本地构建或正式发版；本地允许 ad-hoc，正式版使用 Apple 证书签名与公证，推送版本 tag、发布双架构包到对象存储及 CDN、创建附带安装包的 GitHub Release，并更新 ysicing/homebrew-tap。支持预演与中断恢复，不用于 XStats 或普通功能开发。
---

# 51Code 发版

复用仓库发布脚本，让已安装应用通过自有对象存储获得可信更新，同时在对应 tag 的 GitHub Release 附加双架构 DMG，供用户直接下载安装。

技能调用名保留 `xsourcetree-release`，兼容已有入口。改名后的数据目录、Bundle ID、钥匙串服务名与更新签名密钥必须沿用旧身份，详见 `docs/development.md` 的“改名兼容”；不得批量替换这些兼容标识。

## 模式与授权

- 本地开发或预览使用 `task dev` / `task build`，默认 ad-hoc；本地产物不得用于正式发布。
- 正式发布预演使用 `bun run release --dry-run`，执行双架构 Developer ID 签名、公证及校验；dry-run 仍会向 Apple 提交公证，但不上传对象、不打 tag、不更新 tap。
- 按用户已明确的发布约定，“发布 51Code <版本>”（原 XSourceTree）包含发布源码 commit/push、创建并推送 `v<version>` tag、正式签名与公证、对象/CDN 发布、创建或补齐该 tag 的 GitHub Release 及双架构 DMG 附件、更新并推送 `ysicing/homebrew-tap`。用户明确缩小范围时遵循其限制。
- “整理技能”“准备发版”“预演”“只构建”“只发布已有产物”按请求范围执行；不推断额外发布动作。安装或覆盖用户正在运行的应用不属于默认流程。正式发版只推 tag 不算交付完成，必须同时有可下载的 GitHub Release 安装包附件。
- 目标版本优先采用用户指定值，否则核对 `package.json`、`CHANGELOG.md` 与线上清单。不要静默升版本或覆盖已发布版本。

## 来源与约束

开始前读取适用规则、`git status --short`、暂存内容及已有 `docs/handoff.md`。保留无关修改，尤其不将已有暂存内容混入发布提交。按当前项目实现确认入口与产物，不依赖过时 Web UI 流程。

| 文件 | 用途 |
| --- | --- |
| `package.json` | 应用版本与脚本入口 |
| `CHANGELOG.md` | 版本对应的中文更新说明 |
| `release.config.json` | 下载根地址、`mc` 目标、GitHub 仓库、Ed25519 公钥及 Apple 发布配置 |
| `mygo.config.ts` | 版本、签名和打包配置 |
| `scripts/publish-release.mjs` | 双架构校验、上传顺序、回读与重试约束 |
| `Taskfile.yml`、`docs/development.md` | 验证、签名、发布及原生桌面 smoke 说明 |

每次从配置读取对象目标、下载地址、Developer ID 身份、预期团队 ID 和公证钥匙串配置，不把证书指纹硬编码进技能。默认对象目标为 `c-ip/oss/apps/macOS/51code`，下载根地址为 `https://c.ysicing.net/oss/apps/macOS/51code`；以当前配置为准，不操作 XStats 路径。`legacyUpdates` 指向原 XSourceTree 目录，发布脚本会在新安装包验证后同步旧入口清单；新旧目录的双架构清单都须回读验证，历史对象不移动、不覆盖。使用发布机器已有的 `mc` 别名，不输出凭证。

更新私钥默认位于 `~/.config/xsourcetree/update-keys/mygo-update.key`，通过 `MYGO_UPDATER_PRIVATE_KEY` 传给构建。保留现有密钥；缺失、不可读或不匹配时停止，不生成或更换密钥。不得将私钥写进仓库、技能、输出或命令行字面参数。

正式发布必须具备有效 Developer ID Application 证书及私钥、公证权限。用 `security find-identity -v -p codesigning` 核验证书，用 `xcrun notarytool history --keychain-profile <配置名>` 检查公证权限。正式构建不能回退 ad-hoc；普通本地构建仍可 ad-hoc。

## 准备与验证

1. 目标版本使用 `x.y.z` 稳定版；依据实际代码变化更新 `package.json` 和对应中文 changelog，不将计划或未完成功能写成已发布能力。
2. 确认 Go、Bun、MyGo、`mc` 与 `gh` 可用，使用 `gh auth status --hostname github.com` 确认发布账户具备目标仓库 Release 写权限，遵循锁文件。若 Bun 与锁文件不兼容，选择本机兼容版本并保证嵌套构建使用该版本，不降级锁文件或修改全局 PATH。
3. 同机重任务串行，Go worker 最多 2；资源不足时等待或报告验证缺口，不跳过发布门禁。
4. 运行 `task check` 与 `git diff --check`。`task check` 是当前 Go 测试（包括原生 UI）、发布脚本测试和 Go 静态检查入口；不再运行旧 Web UI 的绑定生成、TypeScript 或 Vite 检查。桌面交互或更新安装流程变化时，按 `docs/development.md` 使用隔离 smoke；不覆盖或终止用户正在运行的应用。
5. 正式发布时，只暂存本版本所需的源码、版本、更新日志与发布配置；核对暂存差异并排除无关内容。提交后记录发布提交 SHA，构建输入必须对应此提交。预演可以不提交，但须记录源码状态，不能把含未提交改动的产物冒充已提交源码的正式包。

有与当前版本、配置和源码匹配的可信产物时可复用；来源不明的历史目录不能仅凭版本号复用。正式构建示例：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  GOMAXPROCS=1 GOFLAGS=-p=1 bun run release --dry-run
```

记录实际生成的 `build/release-*` 目录及其对应源码，后续发布始终使用同一产物。发布脚本以 `XSOURCETREE_RELEASE=1` 启用 Developer ID 签名和公证。预演会向 Apple 提交公证，但不会上传对象，也不检查远端对象冲突或 CDN 可达性。

正式产物逐架构检查：

- 实际更新归档中的 `.app` 和 DMG 均通过严格签名校验；签名为 Developer ID Application，团队 ID 与配置一致，`.app` 启用 Hardened Runtime。
- `.app` 和 DMG 均完成 Apple 公证、`xcrun stapler validate` 与 Gatekeeper `spctl` 检查；DMG 通过 `hdiutil verify`。检查实际更新归档，不以旁边的 `.app` 替代。
- 更新归档及清单齐全，版本正确，下载 URL 使用当前 CDN；Ed25519 更新包验签与 Apple 签名、公证分别验证，不能相互替代。

`release`、`publish` 及 dry-run 都必须拒绝不满足正式 Apple 发布要求的包。预演或准备任务到此交付产物目录、检查结果及限制，不继续外部发布。

## 源码与 tag

正式上传前核对当前分支、上游状态、本地与远端 `v<version>` tag。创建附注 tag，精确指向本次构建对应的发布提交；推送分支及该 tag，不使用 `--tags`，不强推。远端支持时使用原子推送。确认远端分支包含发布提交，且远端 tag 解引用 SHA 与构建提交相同后，才继续上传。发布脚本要求 tag 已存在，不会替代此处的源码及 tag 推送。

只有恢复同一次发布且已确认 tag SHA、版本完全匹配时，才复用已存在 tag。不同 SHA、来源不明或同版本不同内容必须停止，不能移动、删除或覆盖 tag。源码或 tag 推送失败时不推进对象发布。

## 对象存储与 CDN

仅在版本已获发布授权且正式校验完成后发布：

```sh
bun run publish --dir "$RELEASE_DIR" --dry-run
bun run publish --dir "$RELEASE_DIR"
```

复用发布脚本，不手工覆盖固定清单绕过校验。脚本先预检不可变对象及当前清单；远端读取失败不能视为对象不存在，发现更高版本不能回退。随后条件上传带版本号的 DMG、归档和历史清单，完整 CDN 回读校验 SHA-256；同名同内容可重跑，同名不同内容停止。全部版本对象通过后，才更新两个固定清单并回读确认。

同一产品串行发布；脚本预检不是跨进程锁。版本对象不可变且长缓存，固定清单必须使用 `Cache-Control: no-cache,max-age=0,must-revalidate`。检查公开入口实际缓存响应；CDN 强制长缓存时，不可声称客户端已可更新。

## GitHub Release 安装包

`release` 与 `publish` 在对象/CDN 验收后，自动为配置中的 `githubRepository`（当前为 `ysicing/51code`）发布 `v<version>` Release。使用同一正式产物目录，附加：

- `51code-<version>-arm64.dmg`
- `51code-<version>-amd64.dmg`

新 Release 先保持草稿，说明来自已验签更新清单的当前版本更新日志；两个附件上传完成且 GitHub 返回的 SHA-256 digest 与 CDN 使用的本地产物一致后，才公开发布。tag 本身不能承载附件，源码压缩包不代替 DMG。

已有 Release 或中断留下的草稿可用原目录重跑；同名同 SHA-256 附件跳过，仅补传缺失附件。同名不同内容、缺少可验证 digest、读取权限或上传失败时停止，不使用 `--clobber`、不删除附件。CDN 已生效而 GitHub 阶段失败时，保留 tag、清单和产物，用原目录继续，不重建同版本包。`--dry-run` 只展示计划，不创建 Release 或上传附件，也不验证 GitHub 权限或远端 tag。

上传失败可能留下同名、状态为 `starter` 的空占位附件，单纯重跑不能恢复。遇到脚本的占位附件提示时，报告对应仓库、版本和文件名，由维护者核实并手动删除该占位附件后，再用原产物目录重跑。脚本不自动删除；不得删除状态为 `uploaded` 的完整附件来绕过 SHA-256 冲突。

## Homebrew tap

对象与 CDN 验收通过后，更新 `ysicing/homebrew-tap`（tap 名称 `ysicing/tap`）。先核对远端默认分支、适用规则、已有 cask、CI 和本地工作区修改。正式 cask 为 `Casks/51code.rb`，token 为 `51code`；tap 根目录 `cask_renames.json` 保留 `"xsourcetree": "51code"`，兼容旧名称及已有安装的迁移。只维护新 cask，不恢复 `Casks/xsourcetree.rb`，不添加 `deprecate!`。只修改该产品及其安装说明，不改其他产品或添加无关自动化。没有正式发布新安装包时，不提前把线上 cask 指向尚未上传的 URL。

- `version` 与源码 tag、两个固定更新清单一致。
- 用 `arch arm: "arm64", intel: "amd64"` 和对应 `sha256 arm: ..., intel: ...`；URL 为 `<downloadBase>/51code-#{version}-#{arch}.dmg`。
- SHA-256 必须来自本次已发布且 CDN 回读一致的 DMG，不能使用更新归档 hash 或 `sha256 :no_check`。
- 使用 `app "51Code.app"` 和 `auto_updates true`。最低 macOS 版本以当前配置和实际产物为准（目前为 macOS 12 Monterey）。
- 如配置 livecheck，可使用本项目的稳定版 Git tag 或 GitHub Release；下载 URL 仍使用本项目 CDN，不使用 XStats 的发布源。

使用本地 Homebrew 能解析到的 tap 工作区，运行：

```sh
brew style --cask ysicing/tap/51code
brew audit --cask --online --arch=all ysicing/tap/51code
brew info --cask --json=v2 ysicing/tap/51code
brew info --cask --json=v2 ysicing/tap/xsourcetree
brew migrate --cask --dry-run ysicing/tap/51code
```

确认新旧名称均解析为 `51code`，旧 token 出现在改名信息中，双架构 URL 与 SHA-256 保持一致，且没有弃用标记。迁移只预览，不改动用户已有安装；未进行真实安装、迁移或卸载的事项分别报告。这些检查不能代替双架构 DMG 回读和 hash 校验，也不要求安装或覆盖用户应用。检查通过后，单独提交 tap cask 改动并推送实际默认分支；核实远端包含该提交，且公开 cask 的版本、双架构 URL 与 SHA-256 正确。安装入口为 `brew install --cask ysicing/tap/51code`，旧名称通过改名映射兼容。

## 恢复与验收

- 上传、验签、CDN 回读、权限或版本冲突时停止推进，保留产物，说明失败阶段及已生效清单，不无限重试。
- 网络或缓存问题解决后，在原授权范围内复用同一产物重跑发布，不重建已上传版本，不强制覆盖、删除对象或回退清单。
- 第二架构清单失败时，第一架构可能已生效；核实两份清单后用原产物继续，不声称回滚。
- tag 已推送但对象失败，保留 tag 并从原产物恢复；对象已发布但 GitHub Release 或 tap 失败，报告部分完成并补完后续步骤，不重新构建或回退线上版本。
- 历史 ad-hoc 版本不能用正式签名重建后覆盖同名对象；正式签名切换使用明确的新版本。
- 首次启用自动更新前安装的旧应用需要手动安装一次新版本；未上传首个清单时检查更新失败不代表升级成功。

完整正式发布须满足：源码提交及 `v<version>` tag 已推送且与产物对应；双架构 Apple 签名、公证与 Gatekeeper 检查通过；对象/CDN 与两个固定更新清单验收通过；GitHub Release 已公开且双架构 DMG 附件 SHA-256 校验一致；tap 更新已验证并推送。最终用简体中文报告版本、模式、源码提交与 tag、签名公证状态、GitHub Release 及双架构入口、对象和清单状态、tap 提交及验证缺口。未实际运行的跨版本安装和 Intel 桌面行为明确列为未验证。
