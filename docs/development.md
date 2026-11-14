# 开发与发布

[返回首页](../README.md) · [使用说明](usage.md)

## 本地开发

需要 Go **1.27.3+**、Bun、Git 和 Task；macOS 需要 Xcode Command Line Tools。所有任务定义在 [Taskfile.yml](../Taskfile.yml)，运行 `task` 查看列表。

```sh
task setup
task dev
```

界面直接使用 MyGo 的 `ui` 和 `ui/editor` Go 组件，不需要 TypeScript 绑定、Vite 服务或前端资源构建。Bun 仅用于 MyGo 打包工具与发布脚本。Git 请求在后台执行，通过 `Window.Update` 将结果交回界面线程。

界面翻译使用 MyGo 原生 i18n，消息目录位于 `locales/`，源语言为 `zh-Hans`，同时维护 `zh-Hant` 和 `en`。修改消息后运行 `bun run mygo generate` 更新并提交 `locales/catalog_gen.go`，通过 `bun run mygo i18n check` 检查翻译完整性。语言选择由 MyGo 持久化；界面标签使用翻译，已有配置值和 Git 引用保持不变。

文件差异、提交详情、储藏预览和历史搜索使用局部加载状态，不禁用整个工作区；连续选择会取消旧读取，仅允许当前仓库、页面及最后一次请求回填结果。导航、写操作和窗口关闭会取消未完成的读取；写操作等待已取消的读取退出后执行。重复点击已加载的文件保留 Diff 编辑器状态。

退出保护区分可取消的读取与写入：仓库列表刷新、仓库状态刷新和目录扫描不阻止关闭，窗口关闭后统一取消上下文并忽略迟到结果。提交、推送等写操作完成前仍阻止退出，并提示正在执行的任务；不能仅凭界面的忙碌状态阻止退出。

AI 协议适配位于 `internal/commitai/`。空协议保留原有 Magpie 自动发现和手动本机地址行为；自定义服务支持 OpenAI Responses、Chat Completions 和 Claude Messages。自定义服务不继承 `MAGPIE_API_KEY`，不跟随重定向，远程端点只允许 HTTPS。显式填写模型 ID 时不依赖模型列表接口；暂存内容发送范围及生成后的差异校验保持一致。

`internal/aikeychain/` 使用已有 purego 依赖调用 macOS Security.framework，不通过命令行传递密钥。钥匙串服务名为 `com.ysicing.xsourcetree.ai`，账号由协议与规范化 Base URL 的 SHA-256 生成。密钥不参与配置 JSON 序列化；读写在后台完成，失败不降级为明文保存或忽略凭证错误发送请求。设置保留未保存的密钥草稿，保存失败允许重试，关闭前确认放弃，写入期间阻止关闭。

构建 macOS 应用与 DMG：

```sh
task build
```

产物位于 `build/darwin-arm64/`（Intel 机器为 `darwin-amd64`）。本地构建默认使用 ad-hoc 签名；正式发布由发布脚本启用 Developer ID 签名、Hardened Runtime、Apple 公证和票据装订。应用更新包另用 Ed25519 签名校验，不能替代 macOS 的代码签名与公证。

`task build`、`bun run build` 和正式发布统一经过 `scripts/build.mjs`。每次成功构建，构建号递增 1，同次双架构构建共用一个号；失败不会消耗构建号。计数及最近一次成功构建的信息保存在当前工作区的 `.mygo/build-info.json`，不提交 Git；删除该文件或换用新工作区会从 1 开始，正式发布应沿用发布工作区的记录。

正式版本仍由 `package.json` 的 `version` 控制，不随本地构建自动修改。右下角状态栏显示例如 `v0.0.1 (12)`，点击打开“关于”窗口；悬停和“关于”窗口可查看构建号、Git short commit 和构建时间。未提交修改的构建附加 `-dirty`。时间以 UTC 写入构建，界面按本地时区显示；macOS 安装包同时写入 `CFBundleVersion`、`XSourceTreeCommit` 和 `XSourceTreeBuildTime`。更新清单、下载文件名和版本 tag 继续使用正式版本号。`task dev` 和直接 `go build` 不递增打包构建号。

本地构建产物与已安装应用共用单实例锁。再次启动时，新进程退出并激活已有窗口，因此已有应用运行时，打开新产物不会切换到新代码。预览新构建前，应先通过应用菜单正常退出已有版本，再直接打开构建目录中的 `.app`，无需覆盖 `/Applications/XSourceTree.app`。代理不得擅自退出用户启动的进程；需要切换时先说明并取得授权。普通预览不得修改 `XDG_RUNTIME_DIR`、`TMPDIR` 或应用标识来绕过单实例锁；独立数据的 `e2e` 测试仍按下文流程执行。

应用图标源文件为 `resources/icon.png`，MyGo 构建时自动使用。透明底 Logo 位于 `assets/brand/logo.png`，macOS 图标文件位于 `assets/brand/XSourceTree.icns`。

## 验证

`task fmt`（或 `task format`）参考 Gitness，先补齐文件头，再通过 `golangci-lint fmt` 统一执行 gofmt、goimports 和 gci。规则集中在 [.golangci.yml](../.golangci.yml)，导入分组为标准库、项目包、第三方包、匿名导入、点导入。覆盖新建但尚未暂存的 Go 文件，跳过依赖、构建目录和生成文件。

工具固定为 golangci-lint `v2.15.0` 和 addlicense `v1.2.0`。golangci-lint 通过 `go run` 调用，addlicense 安装到 `.mygo/tools/`；首次运行会下载并缓存工具，不覆盖全局安装。任务串行执行，Go 并发限制为 2，工具编译使用 `-p=1`，addlicense 每次只处理一个文件。`task fmt:check` 检查文件头是否缺失及格式差异，不修改源码；`task lint` 使用 goheader 校验 Go 版权头内容，并运行格式检查及 govet。

```sh
task fmt
task fmt:check
task lint
```

`task license`（或 `task gencopyright`）单独补齐版权头，`task license:check` 只检查是否缺失。覆盖项目的 Go、TypeScript、MJS、CSS、HTML 和 YAML 文件，包括测试和平台专用源码；生成文件、代理技能目录、依赖及构建产物跳过。JSON、锁文件、文档和图片不添加代码注释。

文件头使用以下内容，新增文件采用当前年份，已有年份和版权声明不会被批量覆盖。goheader 接受年份或年份区间，避免跨年后要求重写所有文件：

```go
// Copyright 2026 ysicing
// SPDX-License-Identifier: AGPL-3.0-only
```

addlicense 检查所有适用文件的头部是否存在；golangci-lint 的 goheader 严格检查当前平台参与构建的 Go 文件，包含测试及 `e2e` 文件。已有版权头与项目模板不一致时需人工确认来源，不覆盖第三方声明。

`task gomod` 单独运行 `go mod tidy`，不随格式化修改依赖。`task check` 串行执行文件头与格式检查、Go 测试（包含原生界面）、发布脚本测试和 golangci-lint。所有 Git 集成测试只操作测试创建的临时仓库，包括本地 bare 远程仓库，不连接真实远程服务。

```sh
task check
```

可用 `task test:go`、`task test:ui`、`task test:release` 或 `task vet` 单独执行检查，`task test` 依次执行 Go 和发布脚本测试。Go 测试使用 `GOMAXPROCS=2`、`-p=1` 和 `-parallel=2`；开发与构建任务同样限制 Go 构建并发。

加载基准覆盖 100／5000 个变更文件的原生列表重绘、单文件历史／比较读取，以及 1 万提交的首屏、后页和搜索。使用 `GOMAXPROCS=2 go test -p=1 -parallel=2 . ./internal/gitrepo -run '^$' -bench 'Benchmark(NativeChangedFilesFrame|RevisionFileLoading|HistoryLoading)$' -benchtime=3x -count=1` 串行复测；基准仓库由测试临时生成，不访问真实远程。比较相同场景的耗时与分配，避免把冷启动、后台负载或不同数据规模混为优化收益。

原生界面测试使用 MyGo `ui.Tester`，覆盖侧栏切换与键盘导航、文件排序菜单、中文文本输入、统一/并排 Diff、提交草稿、失败 hook 后刷新、逐行暂存、冲突版本保护及刷新失败后的写入保护。设置 `XSOURCETREE_TEST_SCREENSHOTS=/tmp/xsourcetree-native-preview` 可导出浅色、深色和窄窗口截图；该测试不验证系统输入法候选窗。

AI 协议测试使用本地 HTTP 服务；设置交互使用内存凭证替身，不读取用户的密钥。可单独执行 `XSOURCETREE_TEST_KEYCHAIN=1 GOMAXPROCS=2 go test -p=1 -parallel=2 ./internal/aikeychain` 验证原生钥匙串读写；该用例只创建独立服务名下的合成测试记录并在结束时清理，不访问已有记录。

真实桌面端集成测试通过 MyGo 的 automation 插件操作应用。插件仅编译进 `e2e` 构建，并且仅在设置 socket 路径时启用。先启动测试用应用，再在另一个终端执行测试：

```sh
GOMAXPROCS=2 GOFLAGS="-p=1 -tags=e2e" \
  MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock bun run dev
```

```sh
MYGO_AUTOMATION=/tmp/xsourcetree-e2e.sock node scripts/native-smoke.mjs
```

测试先确认窗口类型为原生内容，再验证中文路径添加、中文提交说明、暂存与菜单提交、并排 Diff、分支创建与切换、标签、历史搜索、远程配置及首次发布、储藏、备份后丢弃、原生菜单与分组。推送目标全部为临时本地 bare 仓库。启用自动化时，测试应用使用独立的临时数据目录，退出时清理，不影响普通开发版或正式版的本地仓库列表及恢复副本。自动化输入通过框架内容事件发送，菜单快捷键仍需真实键盘验证。

## 项目结构

以下路径均相对于仓库根目录：

```text
internal/gitrepo/     系统 Git 调用、状态解析与仓库操作，无桌面依赖
internal/workspace/   本地仓库库、扫描、分组与已打开仓库管理，无桌面依赖
main.go              MyGo 生命周期与原生菜单
native_app.go        窗口状态、后台任务与草稿保存
native_ui.go         原生仓库列表、文件、历史、储藏与 Diff 界面
native_selection.go  差异加载、逐块/逐行暂存与历史选择
native_dialogs.go    原生弹窗、仓库分组与扫描
native_actions.go    分支、远程、储藏与冲突操作
service.go           界面复用的 Go 仓库服务
scripts/native-smoke.mjs  真实原生桌面端集成测试
```

Git 命令使用参数数组执行，路径按字面值处理。写操作限定在已打开的仓库内，文件变更操作校验当前状态；取消暂存保留工作区内容。命令设有超时，Diff 禁用外部 diff/textconv 程序，输出和预览大小设有限制。窗口关闭会取消该窗口发起的 Git 请求。

仓库现有的 Git 配置、hooks 和签名配置仍然生效。打开仓库应与直接在该仓库运行 Git 一样，信任其配置与 hooks。

## 应用更新与发布

以下命令均在仓库根目录执行。

### 存储与签名

更新包与清单放在自有对象存储，沿用 XStats 的 `mc` 存储别名和 CDN，使用独立目录。[release.config.json](../release.config.json) 统一维护下载根地址 `https://c.ysicing.net/oss/apps/macOS/XSourceTree`、上传目标 `c-ip/oss/apps/macOS/XSourceTree` 和签名公钥。客户端直接通过 HTTPS 检查与下载，再交由 MyGo 验签安装。存储凭证仅由发布机器的 `mc` 管理。

同一配置的 `macos` 保存 Developer ID 证书指纹、预期团队 ID 和公证钥匙串配置名。当前复用本机的 `XStats` 公证配置，凭据和证书私钥保存在钥匙串中，不写入仓库。证书续期后须更新指纹；更换发布机器时须安装对应证书私钥并配置同名公证凭据。可用 `security find-identity -v -p codesigning` 检查证书、`xcrun notarytool history --keychain-profile XStats` 检查公证权限。

Apple Silicon 和 Intel 分别读取下载根地址下的 `update-darwin-arm64.json` 和 `update-darwin-amd64.json`；当前发布完整更新包，不生成增量包。发布脚本要求两个平台的清单与归档齐全，且版本为 `x.y.z` 稳定版。

版本统一修改 [package.json](../package.json) 的 `version`，MyGo 构建配置、关于窗口与界面版本号均使用该版本；同时在 [CHANGELOG.md](../CHANGELOG.md) 添加对应 `## <version>` 节，构建时写入更新说明。

发布签名私钥必须与 `release.config.json` 中的公钥配对；私钥保存在仓库外并离线备份，不要重新生成替换，否则已安装应用将拒绝新签名。当前维护者本机路径为 `~/.config/xsourcetree/update-keys/mygo-update.key`；其他构建环境通过 `MYGO_UPDATER_PRIVATE_KEY` 提供同一私钥。

### 构建与上传

构建双架构正式发布包，完成 Apple 公证和本地校验（不上传对象存储；公证会将安装包提交给 Apple）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  task release -- --dry-run
```

没有更新私钥的普通构建仍可检查和安装由维护者签名的更新，但不生成可发布的更新清单。发布机器需安装 `mc` 并配置 `c-ip` 别名，同时具备配置中的 Developer ID 私钥和公证凭据。还需安装 `gh`，通过 `gh auth status --hostname github.com` 确认发布账户具备 `release.config.json` 中 `githubRepository` 的 Release 写权限。`task dev`、`task build` 不启用正式签名和公证。

该命令使用全新的 `build/release-*` 目录，避免复用旧产物，并输出本次目录。正式上传前，须提交并推送本版本源码和对应的 `v<version>` tag，确认 tag 指向产物对应的源码提交。确认后发布这份已验证的产物，无需重新构建：

```sh
task publish -- --dir build/release-实际目录 --dry-run
task publish -- --dir build/release-实际目录
```

也可直接构建并发布：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/xsourcetree/update-keys/mygo-update.key)" \
  task release
```

### 发布校验与失败恢复

发布脚本先验证两个架构的版本、URL、大小与 Ed25519 签名，再解开实际更新归档，检查其中的应用和 DMG：必须使用指定团队的 Developer ID Application 签名，应用须启用 Hardened Runtime，公证票据和 Gatekeeper 校验都必须通过。`release`、`publish` 和各自的 `--dry-run` 均执行这些检查；ad-hoc、开发证书、错误团队或未公证产物不能上传。

通过校验后，上传带版本号的 DMG、归档及 `<version>/update-darwin-*.json` 历史清单。版本对象通过条件 PUT 禁止覆盖；已存在且内容相同可重跑，内容不同必须提升版本，包括从 ad-hoc 改为 Developer ID 重新签名的情况。全部版本对象经 CDN 完整回读并验证 SHA-256 后，才更新两份固定更新清单并回读确认；固定清单设置 `Cache-Control: no-cache,max-age=0,must-revalidate`，CDN 需遵守该响应头，不能强制长缓存。

CDN 与更新清单验证通过后，脚本自动创建或补齐 `githubRepository` 中对应 `v<version>` 的 GitHub Release，附加同一产物目录中的 `xsourcetree-<version>-arm64.dmg` 和 `xsourcetree-<version>-amd64.dmg`。新 Release 先创建草稿，使用更新清单内的版本说明，确认两个附件在 GitHub 上的 SHA-256 digest 与本地一致后才公开。已有同名且 SHA-256 相同的附件直接复用，缺失附件补传，内容不同或无法校验时停止，绝不覆盖。tag 本身没有附件，正式发版不能仅有 tag 或 GitHub 自动生成的源码压缩包。

同一产品发布须串行执行，两份架构清单并非原子更新；中途失败可使用同一产物目录重跑，脚本拒绝覆盖新版本清单为旧版本。CDN 已生效但 GitHub 阶段失败会明确报告部分完成；保留原目录重跑，不重建同版本包、不回退清单。若上传失败留下同名 `starter` 占位附件，脚本会停止并提示：先在对应 Release 中核实并手动删除该占位附件，再用原产物目录重跑；脚本不自动删除附件，不允许删除已有完整附件来绕过内容冲突。`--dry-run` 仅列出 GitHub 发布计划，不验证远端 tag 或权限，不创建 Release、不上传附件。脚本不会修改存储权限、创建标签或提交推送 Git。
