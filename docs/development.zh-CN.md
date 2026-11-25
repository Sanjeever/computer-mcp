# 开发与发布

[English](development.md)

本文介绍本地构建、验证、架构决定与发布流程。支持平台和桌面权限见 [README](../README.zh-CN.md)。

Go 版本见 [go.mod](../go.mod)，pnpm 版本见 [package.json](../package.json)。开发时使用 [CI](../.github/workflows/ci.yml) 所选 Node.js 主版本的最新补丁版本。`engines` 字段约束已发布的启动器。在目标 OS 和架构上构建原生程序。macOS 需要 Xcode Command Line Tools。Windows 使用 Go 工具链，无需 cgo。

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm build:native
pnpm smoke
```

启动器构建产物为 `dist/cli.js`。macOS 原生程序为 `build/computer-mcp`，Windows 为 `build/computer-mcp.exe`。[pnpm 工作区配置](../pnpm-workspace.yaml) 在源码安装时排除可选平台包，使这些包发布前也能开展开发。

构建后，在本地验证实际安装包。

```sh
pnpm pack:platform
npm pack --pack-destination artifacts --ignore-scripts
pnpm pack:check
```

使用 `artifacts/` 中本次构建的启动器包及当前平台包。`pack:check` 将它们离线安装到临时目录，通过已安装的启动器运行同一套 stdio 冒烟检查。`smoke` 完成 MCP 握手，检查工具数量与只读状态，再关闭 stdin 并在时限内等待进程退出。该检查不发送桌面输入。

## 架构决定

后端固定在一个 OS 线程上执行，以保证 UIA COM 对象的生命周期。原生 provider 调用可能超出请求时限，工作线程会持有用户级锁直至调用返回，避免操作重叠。参见[请求限制](../README.zh-CN.md#限制与行为)。

`computer_batch` 使用一个任务和一个桌面锁。它在第一笔前计算图片坐标转换，在首次发送时消耗可操作快照，并在任务内保留转换结果。每笔独立校验目标和路径，独立按下、释放鼠标。校验可识别窗口几何与遮挡变化。窗口内部的笔刷和画布属于应用状态，应由调用方在批次之间确认。

进入可能阻塞的原生 provider 前，批次会发布已完成的笔画和当前发送状态。超时响应可以携带这些回执，此时工作线程仍持有桌面锁。最后一次检查取消前，先把即将发送的笔画标为不确定，避免调用方收到 `not_sent` 后该笔仍可能发出。

窗口与元素 ID 跟随原生身份，避免标题或遍历顺序变化后指向错误的输入目标。macOS 在枚举暂时缺失时保留窗口映射，屏幕窗口匹配有歧义时保留 AX 身份。

窗口捕获和屏幕捕获明确区分，因为无遮挡的窗口图像可能与实际输入落点不同。屏幕 OCR 文本可能来自覆盖窗口，因此与原生控件分开返回。Windows npm 分发版缺少 MSIX 包标识，目前通过 macOS Vision 提供 OCR。参见[工具契约](../README.zh-CN.md#工具)。

Windows 元素点击使用 UIA 报告的桌面物理像素坐标。命中其他根窗口时，后端验证它是否通过同进程 owner 链归属目标窗口，并检查该窗口是否包含观察到的元素矩形。UIA 在控件视图内匹配请求的元素，命中图标或文字子节点时可向上找到对应控件。弹层夹具覆盖主窗口之外的控件、无归属的遮挡窗口和弹层内的遮挡控件。

Windows 后续观察通过前台 GUI 线程的菜单状态识别原生菜单，通过 owner 链验证无标题的前台弹层。屏幕截图限制在已验证的菜单边界内。服务在观察后和使用菜单快照输入前，再次验证菜单身份和几何。

## 验证

[CI](../.github/workflows/ci.yml) 在 Linux 上打包一次启动器，保留入口文件的可执行权限。两个原生任务安装并验证这一份 tarball，再各自上传平台包。macOS 直接运行 Go 测试或 vet 时，沿用 CI 中的部署环境变量。[`build:native`](../scripts/native.ts) 会为原生构建设置部署目标。

[服务端测试](../internal/server/) 中涉及桌面的契约验证使用假后端。[翻译测试](../internal/i18n/i18n_test.go) 检查语言键一致性，[启动器测试](../src/launcher.test.ts) 检查平台包选择和缺包错误。原生交互覆盖范围见 [macOS 桌面](../internal/desktop/integration_darwin_test.go)、[macOS 服务端](../internal/server/coordinates_darwin_test.go)、[Windows 桌面](../internal/desktop/integration_windows_test.go)和 [Windows 截图](../internal/desktop/capture_windows_test.go)夹具测试。

原生 UI 验收应在交互式会话中使用专用测试窗口，覆盖 Unicode 输入、点击、键盘选择、滚动、拖动、截图、焦点拒绝及失效引用。多种缩放比例与副显示器须分别验证后，才能宣称相应配置已验证。不要在自动化测试中向个人应用发送输入。

macOS 沿用 CI 的部署环境变量，串行运行夹具测试包。

```sh
COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server
```

Windows 在交互式桌面会话中运行桌面夹具测试包。

```powershell
$env:COMPUTER_MCP_NATIVE_TEST = "1"
go test -p 1 -v ./internal/desktop
Remove-Item Env:COMPUTER_MCP_NATIVE_TEST
```

测试期间保持桌面空闲，让夹具窗口保持焦点。Windows 夹具可在准备阶段点击已确认属于自身的编辑控件以获取前台焦点。macOS 缺少辅助功能或屏幕录制权限时，夹具会报告跳过原因。托管 CI 不启用交互夹具，也不验证权限归属或签名升级。

macOS 的 `stdio drawing batch` 用例以两种方式发送同样的 8 笔、256 个路径点。一种是逐笔调用带 `observe_after` 的 `computer_drag`，另一种是调用一次 `computer_batch`。用例读取夹具事件验证笔画顺序和独立性，并记录首次观察之后的调用数、JPEG 数和耗时。`TestNativeWindowsDrawingBatch` 通过 Windows stdio 服务检查每笔独立的按下、拖拽和释放序列，以及末尾的 JPEG。

Windows 测试机没有 Go 工具链时，可复制 `go test -c ./internal/desktop` 生成的桌面测试程序，以及同一份源码构建的原生服务。运行夹具前，将 `COMPUTER_MCP_TEST_BINARY` 设为服务的绝对路径。后续观察夹具通过 stdio 服务验证所属对话框、Win32 右键菜单和无标题所属弹层。

## 发布

根 [package.json](../package.json) 的版本是唯一来源，原生构建嵌入该版本。同步更新它和两个 `optionalDependencies` 版本，再运行 `pnpm install --lockfile-only`。添加 `docs/releases/X.Y.Z.md` 和 `docs/releases/X.Y.Z.zh-CN.md`，记录用户可见变化、升级说明和实际验证。版本、日志和锁文件变更一同提交。GitHub Release 使用英文日志作为正文。

在仓库根目录运行 `node --import=tsx scripts/release.ts check vX.Y.Z` 核对发布输入。可以在发布提交上运行 [CI](../.github/workflows/ci.yml) 获取构建产物。提交进入 `main` 并准备公开后，推送 `vX.Y.Z` 标签。[Release 工作流](../.github/workflows/release.yml) 先校验标签、版本、平台依赖、日志和 `main` 历史，再调用 CI。

### 首次发布

1. 创建 GitHub 的 `npm` environment，允许 `v*` 标签部署。可设置审批人，让发布任务等待标签产物验收。如果维护者审批自己发起的部署，保留自审权限。参见 [GitHub 部署保护规则](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)。
2. 为启动器和两个平台包建立所有权。包名尚未出现在 npm 时，以 scope 所有者身份登录，对各个已验证的引导 tarball 执行 `npm stage publish <tarball> --access public --tag bootstrap`。引导版本须与正式版不同，暂存会占用该版本号。npm 会公开 `0.0.0-stage` 占位版本，引导 tarball 留在待审队列。参见 [npm 暂存发布](https://docs.npmjs.com/staged-publishing/)。
3. 在**三个包各自的设置**中添加 GitHub Actions Trusted Publisher，填写所有者 `Sanjeever`、仓库 `computer-mcp`、工作流 `release.yml`、environment `npm`，并开启 **Allow npm publish**。在首次 OIDC 成功发布前两天内完成配置。参见 [npm Trusted Publishers](https://docs.npmjs.com/trusted-publishers/)。
4. 在 macOS 和 Windows 的交互式桌面验收最终安装包，检查实际客户端的启动路径，以及由系统处理的权限授予。如果启用了审批，在发布任务等待时下载同一标签运行的 `release-assets`。使用 tarball 前，从标签对应的检出目录执行 `node --import=tsx scripts/release.ts verify vX.Y.Z <目录>` 核对清单。

若占位包无法配置 OIDC，可用标签运行中 `release-assets` 的同一批 tarball 执行一次认证发布。先暂存并批准两个原生包，再暂存并批准启动器，正式版使用 `latest` 标签。发布任务核对 npm 摘要后公开 GitHub Release。手工发布的版本没有工作流自动生成的 npm provenance。正式版上线后，拒绝仍待审的引导版本。下一次 OIDC 发布前配置或刷新 Trusted Publisher。

### 发布与恢复

发布任务下载 `release-assets` 并核对其中的清单，用三个 tarball 创建或恢复 GitHub Release 草稿。通过 npm OIDC 和 provenance 先发布两个原生包，再发布启动器，随后公开 Release。在两个受支持平台上分别运行 `npx -y @sanjeever/computer-mcp@X.Y.Z --version` 验证从 registry 安装，再通过客户端接入并调用 `computer_status`。

发布失败时，在原运行中使用 **Re-run failed jobs**，复用原始 `release-assets`。脚本跳过在 npm 上与原 tarball 摘要相同的版本，并补传草稿中缺失的附件。查询错误、附件未完成或摘要不符，以及 npm 版本摘要冲突都会停止发布。删除未完成的草稿附件前先核实状态。如果原产物过期或 npm 已有同版本的不同字节，先核对已发布内容，必要时准备新版本。npm 版本不可覆盖。

[平台打包脚本](../scripts/platform.ts)会随预编译二进制附上第三方许可。发布工作流目前省略 Apple Developer ID 签名与公证、Windows Authenticode 签名。应实测签名升级时能否保留授权，再将其写为受支持行为。
