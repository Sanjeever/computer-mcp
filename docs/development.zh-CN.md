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

窗口与元素 ID 跟随原生身份，避免标题或遍历顺序变化后指向错误的输入目标。macOS 在枚举暂时缺失时保留窗口映射，屏幕窗口匹配有歧义时保留 AX 身份。

窗口捕获和屏幕捕获明确区分，因为无遮挡的窗口图像可能与实际输入落点不同。屏幕 OCR 文本可能来自覆盖窗口，因此与原生控件分开返回。Windows npm 分发版缺少 MSIX 包标识，目前通过 macOS Vision 提供 OCR。参见[工具契约](../README.zh-CN.md#工具)。

## 验证

[CI](../.github/workflows/ci.yml) 定义完整检查流程，在安装包冒烟检查后上传 tarball。macOS 直接运行 Go 测试或 vet 时，沿用 CI 中的部署环境变量。[`build:native`](../scripts/native.ts) 会为原生构建设置部署目标。

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

## 发布

根 [package.json](../package.json) 的版本是唯一来源。原生构建嵌入该版本，平台打包时据此检查 `optionalDependencies` 的版本。同步更新这些字段后运行 `pnpm install --lockfile-only`。提交清单、锁文件及受影响的中英文文档。

[Release](../.github/workflows/release.yml) 工作流提供两个入口。

- 推送 `vX.Y.Z` 标签，构建、验证、发布三个 npm 包并创建 GitHub Release。
- 手动运行 Release 工作流，设置 `publish=false`，生成和上传产物，不公开发布。设置 `publish=true` 时须选择与版本匹配的标签。

标签须匹配 `package.json`。工作流使用 CI 产出的 tarball，先发布原生平台包，再发布启动器。npm 版本不可覆盖。重跑失败的发布任务前，先检查哪些包已发布。

首次公开发布前，为 [package.json](../package.json) 中 `name` 和 `optionalDependencies` 列出的包建立所有权。首次创建包可能需要带认证的引导发布。为对应仓库和[发布工作流](../.github/workflows/release.yml)配置 npm trusted publishing，使用发布任务声明的 GitHub environment。创建该环境并设置保护规则。工作流使用 OIDC 和 npm provenance。

[平台打包脚本](../scripts/platform.ts)会随预编译二进制附上第三方许可。发布工作流目前省略 Apple Developer ID 签名与公证、Windows Authenticode 签名。应实测签名升级时能否保留授权，再将其写为受支持行为。
