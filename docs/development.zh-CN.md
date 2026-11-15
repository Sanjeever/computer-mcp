# 开发与发布

[English](development.md)

使用 `go.mod` 指定的 Go 版本、Node.js 22+ 及 `package.json` 指定的 pnpm。macOS 构建需要 Xcode Command Line Tools，并设置 `MACOSX_DEPLOYMENT_TARGET=14.0`；Windows 使用 Go 工具链，无需 cgo。用户机器无需编译器。

```sh
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm fmt:check
pnpm test
go test ./...
go vet ./...
pnpm build
pnpm build:native
pnpm smoke
pnpm pack:platform
```

macOS 执行 Go test/vet 前设置以下变量，`build:native` 会自动设置：

```sh
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_CFLAGS="-mmacosx-version-min=14.0"
export CGO_LDFLAGS="-mmacosx-version-min=14.0"
```

本机 Go 较旧时，工具链可能下载 `go.mod` 要求的版本。首次发布平台包前，pnpm 可能报告缺少可选平台依赖，本地构建和冒烟验证直接使用 `build/` 产物。

Go 测试覆盖快照身份、失效目标拒绝、图片坐标映射、输入校验、密码值省略、MCP Schema 拒绝未知参数和翻译完整性。TypeScript 测试覆盖平台选择及缺少平台包的错误。`smoke` 启动真实 stdio 进程，完成 MCP 握手、工具列表、只读状态查询及 EOF 退出检查，不发送桌面输入。

后端在固定 OS 线程串行执行。UIA 使用 COM MTA、属性缓存、provider 超时和有界遍历；macOS 使用带消息超时的 AX 查询与 ScreenCaptureKit。原生引用保留在后端内。快照包含坐标元数据，语义观察与截图依次采集，不构成桌面的原子视图。

`internal/i18n/en.json` 与 `zh-CN.json` 必须拥有相同键。中英文 README 描述相同行为。保留系统错误细节，提交信息采用英文 Conventional Commits。

## CI

`.github/workflows/ci.yml` 检查 Go 和 TypeScript，在 macOS arm64 和 Windows x64 上构建、运行 stdio 冒烟检查、打包原生 npm 包，再将原生包和启动器 tarball 安装到独立临时目录执行冒烟检查。上传的产物就是通过验证的 tarball。托管 runner 不验证 macOS 授权、签名升级或任意应用交互。

原生 UI 验收应在交互式会话中使用专用测试窗口，覆盖 Unicode 输入、点击、键盘选择、滚动、拖动、截图、焦点拒绝及失效引用。多种缩放比例与副显示器须分别验证后，才能宣称相应配置已验证。不要在自动化测试中向个人应用发送输入。

macOS 设置上述 cgo 参数后，可运行 `COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server`。原生测试包需串行执行，避免专用窗口争抢焦点。这些显式启用的测试创建专用 Cocoa 窗口，验证焦点、英文／中文／emoji 输入、多行富文本编辑（含开头换行、制表符、选区替换及快捷键后的插入）、点击、子树身份、300 行表格文本提取、改标题和隐藏恢复后的窗口 ID、模态阻塞、遮挡下的窗口与屏幕截图、裁剪尺寸、落点拒绝及自绘控件的中英文 Vision OCR。服务端测试从实际 JPEG 定位目标，验证原尺寸、缩放和裁剪截图，以及窗口移动和调整大小后的图片坐标点击。缺少辅助功能或屏幕录制权限时，测试会报告跳过原因。目前原生交互夹具覆盖 macOS，Windows CI 执行原生状态与 stdio 检查。Windows 静态检查不能证明 PrintWindow 或 UIA 运行时兼容性。

服务端夹具还验证非键盘焦点浮动面板、多点连续笔画、取消后的鼠标释放，以及通过真实 stdio 子进程截图。单元测试覆盖原有两点拖动、路径预算、裁剪与负坐标转换、中间路径点遮挡及移动失败不重试。

## 发布

根 `package.json` 的版本是唯一来源。同步更新根版本和两个 `optionalDependencies` 版本，然后运行 `pnpm install --lockfile-only`。提交锁文件及受影响的中英文文档。

发布入口有两种：

- 推送 `vX.Y.Z` 标签，构建、验证、发布三个 npm 包并创建 GitHub Release。
- 手动运行 Release 工作流，设置 `publish=false`，生成和上传产物，不公开发布。设置 `publish=true` 时须选择与版本匹配的标签。

标签须匹配 `package.json`。工作流先发布原生平台包，再发布启动器，使用 CI 验证过的 tarball，不重新构建。npm 版本不可覆盖；发布部分失败时，重跑前先检查已经发布的包。

首次公开发布前，需要建立以下包的所有权，并配置 npm trusted publisher，仓库为 `Sanjeever/computer-mcp`，工作流为 `release.yml`，GitHub environment 为 `npm`：

- `@sanjeever/computer-mcp`
- `@sanjeever/computer-mcp-darwin-arm64`
- `@sanjeever/computer-mcp-win32-x64`

在 GitHub 创建 `npm` environment 并配置保护规则。初次创建包和 npm 账户配置可能需要单独进行带认证的引导发布，属于工作流之外的账户操作。之后通过 OIDC、`id-token: write` 和 npm provenance 发布。工作流不嵌入或依赖 `NPM_TOKEN`。

原生二进制链接系统框架，平台包包含第三方许可。目前工作流不执行 Apple Developer ID 签名与公证，也不执行 Windows Authenticode 签名。接入可信签名需要相应账户、证书和 CI secret 配置；宣称升级后授权稳定前，应完成实测。
