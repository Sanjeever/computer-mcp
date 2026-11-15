# Computer MCP

[English](README.md)

通过 stdio MCP 为 Agent 提供原生桌面观察与操作。Go 实现核心，小型 JavaScript 启动器选择 npm 包内的预编译二进制。

支持 **macOS 14+ Apple Silicon** 和 **Windows 10/11 x64**。npm 启动器要求 Node.js 22+。桌面交互需要用户已登录图形会话。

## 接入

首次发布到 npm 后，在支持 `mcpServers` 的客户端中配置：

```json
{
  "mcpServers": {
    "computer": {
      "command": "npx",
      "args": ["-y", "@sanjeever/computer-mcp"],
      "env": { "COMPUTER_MCP_LANG": "zh-CN" }
    }
  }
}
```

语言默认使用英文。设置 `COMPUTER_MCP_LANG=zh-CN` 或传入 `--lang zh-CN`，可切换工具描述和应用错误信息。工具名、字段和错误码保持不变。桌面原文不翻译，原生诊断与 SDK 参数校验信息可能使用英文或系统语言。

Windows 客户端若不能直接启动 `npx.cmd`，可使用 `cmd`，参数为 `[/d, /s, /c, npx, -y, @sanjeever/computer-mcp]`，实际配置写成 JSON 字符串数组。具体遵循客户端进程配置文档。本地开发可直接配置 `build/computer-mcp` 或 `build/computer-mcp.exe` 的绝对路径。

上述包名是预期公开入口；克隆或构建仓库不代表已经发布到 npm。

## 权限

macOS 在“系统设置 → 隐私与安全性”中为系统显示的负责宿主或可执行程序授予**辅助功能**和**屏幕录制**权限。修改权限后重启 MCP。`computer_status` 检查权限，不弹出授权窗口。终端与 Agent 客户端的权限归属可能不同，应验证实际启动路径。npm 产物尚未经过 Developer ID 公证，升级后可能需要重新授权。

Windows 应运行在目标用户的交互式会话中。UIPI 可能阻止向管理员应用发送输入。首版不支持锁屏、UAC 安全桌面或无人值守服务会话中的交互。

## 工具

| 工具               | 用途                             |
| ------------------ | -------------------------------- |
| `computer_status`  | 平台、权限或能力检查、显示器坐标 |
| `computer_window`  | 列出、激活、移动和调整窗口       |
| `computer_observe` | 有界无障碍树及可选 JPEG 截图     |
| `computer_click`   | 点击快照元素或图片坐标           |
| `computer_type`    | 在当前焦点插入 Unicode 文本      |
| `computer_key`     | 发送一组组合键                   |
| `computer_scroll`  | 在快照图片坐标处滚动             |
| `computer_drag`    | 在两个快照图片坐标之间拖动       |
| `computer_wait`    | 等待窗口或无障碍文本匹配         |

先调用 `computer_window({"action":"list"})`，激活精确 `window_id`，再观察窗口。截图坐标相对于返回图片，左上角为 `(0, 0)`。`region` 和窗口边界使用桌面坐标，macOS 为逻辑点，Windows 为 DPI 感知像素，由服务完成转换。

窗口操作也支持 `by_app`（应用名称或 `app_id`，忽略大小写精确匹配）和 `by_title`（忽略大小写的标题子串）。可组合筛选列表；activate/move 遇到多个匹配返回 `ambiguous_window`。筛选不能与 `window_id` 同用。macOS 窗口 ID 根据原生窗口身份跟踪，不依赖标题；原生窗口仍存在时，暂时枚举缺失会保留映射。原生匹配有歧义时保留 AX 身份，不根据标题猜测。

macOS 浮动工具面板作为独立窗口列出。应按面板自身的 `window_id` 观察和操作，不能沿用文档窗口的截图坐标。应用处于前台时，非键盘焦点面板可返回 `mouse_active:true`、`active:false`。激活操作可确认这一可接收鼠标的状态，无须让面板取得键盘焦点；键盘输入仍要求 `active:true`。窗口鼠标操作继续检查几何位置和落点实际所属窗口。

```json
{ "window_id": "<window_id>", "screenshot": true, "max_elements": 200 }
```

调用 `computer_click` 时提供响应中的 `snapshot_id` 和 `element_id`；若已请求截图，也可改用 `point: {"x": 120, "y": 80}`。两种目标必须且只能提供一种。`computer_type` 与 `computer_key` 要求目标 `window_id` 已激活。文本插入不替换已有内容，不修改剪贴板。替换前请显式全选，macOS 为 `["meta", "a"]`，Windows 为 `["ctrl", "a"]`，然后输入文本。

输入后重新观察。新观察、等待、窗口修改或输入都会使旧快照失效；未使用的快照在 60 秒后过期。所有窗口和桌面共享这一规则，桌面观察也会替换窗口快照。遇到 `stale_snapshot` 时，重新观察预期目标，使用新引用和新截图坐标。输入前检查几何位置、焦点和元素属性，这些检查不能消除与外部桌面变化之间的竞争窗口。

操作 Dock、任务栏、菜单或无窗口桌面时，使用 `computer_observe({"desktop":true})`。该模式返回首个显示器截图，不包含窗口或语义元素。可根据 `computer_status` 的桌面坐标设置 `region`，选择其他显示器或跨显示器截图。桌面快照坐标输入检查显示器布局，不要求窗口焦点。窗口列表覆盖可见、非最小化的应用窗口。

`computer_wait` 接受精确 `window_id`、可选 `text` 和不超过 10,000 的 `timeout_ms`。超时返回 `matched: false`。文本匹配无障碍名称和非密码值，不执行 OCR。

激活会等待窗口焦点或非键盘焦点面板的鼠标操作条件确认，默认 2,000 毫秒，可通过 `timeout_ms` 设置，最多 10,000。失败返回 `activation_failed`，已识别的模态阻塞返回 `window_blocked`。`computer_wait` 还支持 `active:true`、`focused:true` 和精确 `focus_name`，可与 `text` 组合。键盘工具支持 `auto_activate:true` 和 `focus_name`，在发送前准备目标，共享默认 2,000 毫秒的准备预算。输入发送一次，成功表示已投递，不表示应用处理完成。基于快照的鼠标输入仍要求显式激活后重新观察。

激活窗口不会选择文本框。打开查找栏或对话框后，应检查观察结果中 `focused` 的元素。`focus_name` 是该控件的无障碍 `name`，不是窗口标题；控件无名称时省略。该参数等待焦点，不会转移焦点。键盘工具返回 `focus_timeout` 时，会附带最近观测的控件名称、角色和焦点状态，且未发送输入。`computer_wait` 的条件等待超时则返回 `matched:false`。

观察支持 `text`、`role`、`region`、`compact:true` 和 `max_depth`（1～64）。不截图时也可用 `region` 筛选相交的语义元素。通过 `root_id` 和当前 `snapshot_id` 可限定子树。遍历先访问后代再访问兄弟节点，最多扫描 5,000 个节点，保留不超过 `max_elements` 个可操作引用。macOS 还会读取关联 AX 标签及有界的行内后代文本，补充列表内容。

传入同窗口、同筛选条件的最新观察 ID `since_snapshot_id` 可获取差异。此时 `elements` 包含新增和变化节点，移除 `removed_element_ids` 后按 `element_order` 重建当前有界结果。`diff_complete:false` 表示至少一次观察发生截断，节点从结果消失不代表从应用消失。后续操作均须使用新 `snapshot_id`，未变化的元素也一样。原生身份变化表示为删除和新增。差异减少响应大小，不减少原生遍历工作。

macOS 可通过 `ocr:true` 对返回截图执行本地 Vision 识别，窗口观察需同时设置 `screenshot:true`。结果包含有数量上限的 `ocr` 文本区域、置信度和图片坐标，超出预算时标记 `ocr_truncated`。无名称且无 AX 值的控件可获得 `ocr_text`，原生名称和点击校验保持独立。OCR 结果会省略与语义结果中密码控件相交的文本；其他可见敏感文本仍可能出现，尤其是纯截图或筛选观察。OCR 按需启用，会增加识别耗时，不上传图片。`computer_status.ocr` 表示支持状态，当前 npm Windows 分发版请求 OCR 会返回 `unsupported_operation`。

## 限制与行为

OCR 仅在窗口捕获模式下补充原生控件。屏幕截图中的文字可能来自覆盖窗口，因此以独立 OCR 区域返回。

- 默认观察 200 个元素，最多 1,000 个。树遍历预算为三秒，未完成时返回 `truncated`。名称和值均有长度上限，原生 provider 阻塞可能超出遍历预算。
- 截图默认最大宽度 1,440，可设为 320～2,560，高度不超过 2,560。窗口观察默认 `capture_mode:"window"`，macOS 使用 ScreenCaptureKit，Windows 使用 PrintWindow。`capture_mode:"screen"` 采集可见桌面区域及覆盖窗口，桌面模式固定使用 screen。Windows 应用需支持 PrintWindow 渲染，不会自动降级为屏幕截图。截图以 MCP 图片内容返回，不写入磁盘。窗口源缓冲区及 Windows 屏幕区域最多 4,000 万像素。
- 窗口鼠标输入检查点击、滚动位置及拖动两端实际所属窗口。窗口截图可看到被遮挡内容，但不允许穿透覆盖窗口点击，检查失败返回 `target_obscured`。检查仍可能与外部变化发生竞争。
- 保留最新快照，原生引用受元素数量限制。单个固定线程执行桌面请求，最多排队 16 项。总请求预算为 15 秒，包括排队时间。
- 用户级文件锁协调本服务的多个实例，不能阻止人或其他软件切换焦点。`busy` 表示其他实例正在执行桌面操作。
- 点击使用真实鼠标事件。组合键使用虚拟键；macOS 字母与数字快捷键对应 US 键盘物理位置。Unicode 文本插入不依赖这些快捷键。自绘控件可能需要图片坐标和具备视觉能力的 Agent。
- 工具成功表示输入已发送或系统调用已接受，不保证应用完成预期任务。超时和取消不会触发输入自动重试。
- 观察和等待超时会明确说明本次未发送输入。macOS 上，同一可执行路径存在多个存活进程时，ScreenCaptureKit 可能一直等待图像回调。若持续发生，请停止其他 MCP 实例，再重启受影响的实例。超时的原生操作在返回前仍占用工作线程。
- 语义快照省略密码值，截图仍可能包含屏幕上的敏感内容。Agent 应将桌面文本视为不可信输入。

本地构建、验证范围与 npm 发布准备见[开发与发布](docs/development.zh-CN.md)。贡献约定见 [AGENTS.md](AGENTS.md)。

[MIT](LICENSE) © Sanjeever。
