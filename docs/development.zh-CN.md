# 开发与发布

[English](development.md)

本文介绍本地构建、验证、架构决定与发布流程。支持平台和桌面权限见 [README](../README.zh-CN.md)。

使用 [go.mod](../go.mod) 指定的 Go 版本，以及 [package.json](../package.json) 中 `engines` 和 `packageManager` 指定的 Node.js 与 pnpm 版本。macOS 构建需要 Xcode Command Line Tools；Windows 使用 Go 工具链，无需 cgo。原生二进制在对应 OS 和架构上构建。

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm build:native
pnpm smoke
```

类型检查、lint、格式检查、Vitest、Go 测试与 vet、安装包验证的完整命令和顺序以 [CI](../.github/workflows/ci.yml) 为准。macOS 直接执行 Go test/vet 前设置以下变量，`build:native` 会自动设置：

```sh
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_CFLAGS="-mmacosx-version-min=14.0"
export CGO_LDFLAGS="-mmacosx-version-min=14.0"
```

本机 Go 较旧时，工具链可能下载 `go.mod` 要求的版本。[pnpm 工作区配置](../pnpm-workspace.yaml) 在源码依赖安装时排除本项目的可选平台包，保证这些包发布前也能完成冻结安装。本地构建和冒烟验证直接使用 `build/` 产物，安装包检查会安装本次构建的 tarball。

构建后，在本地验证实际安装包：

```sh
pnpm pack:platform
npm pack --pack-destination artifacts --ignore-scripts
pnpm pack:check
```

`pack:check` 需要两个 tarball，将它们离线安装到独立临时目录，再通过启动器执行 stdio 冒烟检查。`smoke` 完成 MCP 握手、工具列表、只读状态查询及 EOF 退出检查，不发送桌面输入。

## 架构决定

后端在固定 OS 线程执行，保证 UIA COM 对象生命周期与桌面操作串行。UIA 和 AX 使用有界遍历与 provider 超时，但原生调用仍可能超出请求时限。原生调用返回前，工作线程和进程间锁保持占用，防止其他操作与其重叠。请求和队列上限见 [README](../README.zh-CN.md#限制与行为)。

原生引用保留在 `internal/desktop` 内。窗口与元素 ID 跟随原生身份，避免修改标题或遍历顺序变化后将输入发送给错误目标。macOS 窗口映射可跨暂时枚举缺失保留；匹配有歧义时保留 AX 身份，不根据标题猜测。

窗口捕获与屏幕捕获须显式选择，因为无遮挡的窗口图像可能与实际输入落点不同。落点检查仍会拒绝被覆盖的窗口目标。屏幕 OCR 与原生控件分开返回，因为文字可能来自覆盖窗口。捕获模式、坐标、焦点和快照恢复规则见[工具契约](../README.zh-CN.md#工具)。

## CI

[CI](../.github/workflows/ci.yml) 在两个支持平台上执行检查和安装包冒烟测试，再上传已验证的 tarball。托管 runner 不验证 macOS 授权、签名升级或任意应用交互。

- [服务端测试](../internal/server/) 覆盖快照失效、坐标映射、焦点与输入校验、取消、密码省略、OCR、差异及 MCP 契约，涉及桌面交互时使用假后端。[翻译测试](../internal/i18n/i18n_test.go) 检查语言键一致性，[启动器测试](../src/launcher.test.ts) 检查平台选择与缺少平台包的错误。
- [桌面夹具测试](../internal/desktop/integration_darwin_test.go) 覆盖 macOS 原生输入、窗口身份、文本提取、截图与 OCR。[服务端夹具测试](../internal/server/coordinates_darwin_test.go) 根据实际 JPEG 目标验证缩放、裁剪、窗口移动后的点击，以及浮动面板、连续拖动、取消清理和真实 stdio 截图。
- [Windows 夹具测试](../internal/desktop/integration_windows_test.go) 使用专用 Edit 和 RichEdit 控件，覆盖 Unicode、多行输入、UIA 值与焦点及原生激活。[截图测试](../internal/desktop/capture_windows_test.go) 无需桌面即可检查 JPEG 尺寸，并在交互夹具中验证 PrintWindow 与屏幕截图的裁剪和缩放。RichEdit 的 UIA 值使用 CR 分隔段落，并包含末尾段落标记，不一定与应用保存文件的换行格式一致。

原生 UI 验收应在交互式会话中使用专用测试窗口，覆盖 Unicode 输入、点击、键盘选择、滚动、拖动、截图、焦点拒绝及失效引用。多种缩放比例与副显示器须分别验证后，才能宣称相应配置已验证。不要在自动化测试中向个人应用发送输入。

macOS 设置上述 cgo 参数后运行：

```sh
COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server
```

Windows 在交互式桌面会话中运行：

```powershell
$env:COMPUTER_MCP_NATIVE_TEST = "1"
go test -p 1 -v ./internal/desktop
Remove-Item Env:COMPUTER_MCP_NATIVE_TEST
```

原生测试包需串行执行，避免专用窗口争抢焦点，测试期间保持桌面空闲。Windows 夹具在准备阶段可能显式点击已确认属于自身的编辑控件以获取前台焦点；生产环境的激活操作不会合成输入。macOS 缺少辅助功能或屏幕录制权限时，测试会报告跳过原因。CI 不启用交互夹具；Windows 状态与 stdio 检查不能证明任意应用、字体、缩放比例或显示器配置均兼容。

## 发布

根 [package.json](../package.json) 的版本是唯一来源，原生构建嵌入该版本，平台打包时检查一致性。同步更新根版本和两个 `optionalDependencies` 版本，然后运行 `pnpm install --lockfile-only`。提交锁文件及受影响的中英文文档。

[Release](../.github/workflows/release.yml) 工作流提供两个入口：

- 推送 `vX.Y.Z` 标签，构建、验证、发布三个 npm 包并创建 GitHub Release。
- 手动运行 Release 工作流，设置 `publish=false`，生成和上传产物，不公开发布。设置 `publish=true` 时须选择与版本匹配的标签。

标签须匹配 `package.json`。工作流先发布原生平台包，再发布启动器，使用 CI 验证过的 tarball，不重新构建。npm 版本不可覆盖；发布部分失败时，重跑前先检查已经发布的包。

首次公开发布前，需要建立启动器包（[package.json](../package.json) 的 `name`）及其 `optionalDependencies` 中两个包的所有权。配置 npm trusted publisher，仓库以该文件为准，工作流为 `release.yml`，GitHub environment 为 `npm`。

在 GitHub 创建 `npm` environment 并配置保护规则。初次创建包和 npm 账户配置可能需要单独进行带认证的引导发布，属于工作流之外的账户操作。之后通过 OIDC、`id-token: write` 和 npm provenance 发布。工作流不嵌入或依赖 `NPM_TOKEN`。

原生二进制链接系统框架，平台包包含第三方许可。目前工作流不执行 Apple Developer ID 签名与公证，也不执行 Windows Authenticode 签名。接入可信签名需要相应账户、证书和 CI secret 配置；宣称升级后授权稳定前，应完成实测。
