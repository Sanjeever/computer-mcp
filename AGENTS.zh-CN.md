# Computer MCP

[English](AGENTS.md) 是主要约定。用户契约见 [README.zh-CN.md](README.zh-CN.md)，构建与发布见[开发文档](docs/development.zh-CN.md)。文档以英文为主，用户可见变化同步维护简体中文版本。提交使用英文 Conventional Commits，许可证为 MIT。

## 架构

- Go 负责 stdio MCP、工具契约、桌面状态、有界执行和原生适配，使用官方 Go MCP SDK。
- TypeScript 负责 npm 启动器与打包脚本，编译为 JavaScript 后继承 stdio，直接启动平台二进制，不使用 Shell。使用 pnpm、TypeScript、Oxlint、Oxfmt，TypeScript 单元测试使用 Vitest。
- 发布 macOS arm64 和 Windows x64。macOS 最低 14，使用 cgo 和 Apple SDK；原生构建在对应 OS 上执行。
- 原生访问集中在 `internal/desktop`。macOS 使用薄 Objective-C 桥接，Windows 使用生成的 Win32/UIA 绑定。后端调用和 COM 对象生命周期固定在引擎的 OS 线程。
- 沿用后端边界，新增插件注册机制、服务、网络传输或语言须有具体需求。

## 行为契约

- stdout 专用于 MCP，诊断写 stderr。默认不记录输入文本、截图或完整桌面快照。
- 工具名、字段、ID 和错误码不随语言变化，字段采用 snake_case。工具描述和应用错误从 `internal/i18n` 加载，支持 `en`、`zh-CN`，默认英文。原生诊断可保留系统原文。
- 观察必须有预算并报告截断。元素引用属于最新快照，过期后不可使用；输入消耗快照。窗口快照核对几何位置、焦点和元素身份；纯截图桌面快照核对显示器布局，支持系统 UI 交互。
- 图片坐标相对于裁剪和缩放后的返回图片，在统一位置转换。保留负数桌面坐标与多 DPI 语义。
- 输入串行执行，通过用户级进程间锁协调。结果不确定时不重试。取消不能撤销已发送输入，合成按键和鼠标按钮必须释放。
- 原生 provider 可能超出 Go context 时限。原生调用返回前保留 worker 占用，不能在超时后启动重叠操作。
- 不在无障碍动作与鼠标输入之间静默切换，不增加剪贴板、Shell、浏览器 DOM、网络访问、遥测或权限弹窗。
- 启动时检查权限，不主动申请授权。自动化测试不检查无关个人应用，交互测试使用专用窗口。

## 验证与发布

- 按改动运行 Go 测试与 vet、TypeScript 类型检查、Oxlint、Oxfmt、Vitest、构建和安装包冒烟检查。
- 测试应覆盖失效引用、坐标转换、输入校验、取消、翻译键一致性、MCP 契约和实际安装包启动。
- 交叉编译不等于原生运行验证。托管 CI 不证明交互桌面兼容性或 macOS TCC 授权行为，报告实际测试范围。
- 根 `package.json` 是应用版本来源，平台依赖版本和发布标签须一致，原生版本号由其生成。
- 分发预编译二进制及第三方许可，不使用安装 hook 编译或下载二进制，不将 npm 凭据写入源码。
- 已授权本地实现、测试、打包和提交。推送、发布标签、GitHub Release、npm 发布及账户设置变更，须获得当前会话明确授权。
