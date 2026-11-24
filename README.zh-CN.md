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

Windows 客户端若不能直接启动 `npx.cmd`，可设置 `"command": "cmd"` 和 `"args": ["/d", "/s", "/c", "npx", "-y", "@sanjeever/computer-mcp"]`。具体遵循客户端进程配置文档。本地开发先[从源码构建](docs/development.zh-CN.md)，再配置 `build/computer-mcp` 或 `build/computer-mcp.exe` 的绝对路径。

上述包名是预期公开入口；克隆或构建仓库不代表已经发布到 npm。

## 权限

macOS 在“系统设置 → 隐私与安全性”中为系统显示的负责宿主或可执行程序授予**辅助功能**和**屏幕录制**权限。修改权限后重启 MCP。`computer_status` 检查权限，不弹出授权窗口。终端与 Agent 客户端的权限归属可能不同，应验证实际启动路径。npm 产物尚未经过 Developer ID 公证，升级后可能需要重新授权。

Windows 应运行在目标用户的交互式会话中。UIPI 可能阻止向管理员应用发送输入。不支持锁屏、UAC 安全桌面或无人值守服务会话中的交互。

## 工具

| 工具               | 用途                               |
| ------------------ | ---------------------------------- |
| `computer_status`  | 平台、权限或能力检查、显示器坐标   |
| `computer_window`  | 列出、激活、移动和调整窗口         |
| `computer_observe` | 有界无障碍树及可选 JPEG 截图       |
| `computer_click`   | 点击快照元素或图片坐标             |
| `computer_move`    | 移动鼠标到图片坐标，可选悬停       |
| `computer_type`    | 在当前焦点插入 Unicode 文本        |
| `computer_key`     | 发送一组组合键                     |
| `computer_scroll`  | 在快照图片坐标处滚动               |
| `computer_drag`    | 在快照图片坐标中直线或连续路径拖动 |
| `computer_wait`    | 等待窗口或无障碍文本匹配           |

先调用 `computer_window({"action":"list"})`，激活精确 `window_id`，再观察窗口。截图坐标相对于返回图片，左上角为 `(0, 0)`。`region` 和窗口边界使用桌面坐标，macOS 为逻辑点，Windows 为 DPI 感知像素，由服务完成转换。

窗口操作也支持 `by_app`（应用名称或 `app_id`，忽略大小写精确匹配）和 `by_title`（忽略大小写的标题子串）。可组合筛选列表；activate/move 遇到多个匹配返回 `ambiguous_window`。筛选不能与 `window_id` 同用。窗口 ID 是原生窗口身份的不透明引用。单纯修改标题不会改变 ID。

macOS 浮动工具面板作为独立窗口列出。应按面板自身的 `window_id` 和截图观察和操作。应用处于前台时，非键盘焦点面板可返回 `mouse_active:true`、`active:false`。激活操作可确认这一可接收鼠标的状态，无须让面板取得键盘焦点；键盘输入仍要求 `active:true`。

```json
{ "window_id": "<window_id>", "screenshot": true, "max_elements": 200 }
```

调用 `computer_click` 时提供响应中的 `snapshot_id` 和 `element_id`；若已请求截图，也可改用 `point: {"x": 120, "y": 80}`。两种目标必须且只能提供一种。`computer_type` 与 `computer_key` 要求目标 `window_id` 已激活。文本按应用行为插入当前光标位置或替换当前选区，剪贴板保持不变。替换全文前请显式全选，macOS 为 `["meta", "a"]`，Windows 为 `["ctrl", "a"]`，然后输入文本。

Windows 上，`computer_type` 将每个 LF、CRLF 或单独 CR 作为一次 Enter 按键发送。多行编辑器通常插入换行，其他控件可能提交表单或执行自身的 Enter 操作。

输入后使用新观察。新观察、等待、窗口修改或输入都会使旧快照失效；未使用的快照在 60 秒后过期。所有窗口和桌面共享这一规则，桌面观察也会替换窗口快照。遇到 `stale_snapshot` 时，重新观察预期目标，使用新引用和新截图坐标。输入前检查几何位置、焦点和元素属性，这些检查不能消除与外部桌面变化之间的竞争窗口。

调用 `computer_click` 或 `computer_drag` 时将 `observe_after` 设为空对象，即可从 `observation` 取得输入后的窗口观察。在 `observe_after` 中加入 `"screenshot":true` 可附带 JPEG，下一步使用返回的 `observation.snapshot_id` 和本次图像。同一窗口的新截图沿用捕获模式和宽度上限。原裁剪区域相对窗口仍有效时，也会继续沿用。桌面后续观察要求 `"screenshot":true`，沿用桌面区域和宽度上限。

在 Windows 上，后续观察可以跟随输入目标直属的前台窗口，也可以在对话框关闭后返回所属窗口。服务会短暂等待应用处理输入。界面切换较慢时，使用 `computer_wait` 或重新观察。若同时返回 `input_delivered:true` 和 `observation_error`，输入已发送。请先取得新观察，再决定是否重复输入。

输入尝试进入原生发送阶段后会消耗快照，包括结果不确定的原生失败。参数或输入前置条件校验阶段拒绝请求时，不一定消耗快照。不能根据调用成功或失败判断快照是否仍有效。

在 macOS 上，使用 `capture_mode:"screen"` 取得包含已打开菜单的窗口截图。无障碍接口能确认菜单归属时，可在图内用坐标点击菜单或移动鼠标。菜单超出窗口或归属无法确认时，观察可见菜单周围的小范围桌面区域。单独列出的浮动面板使用自身的窗口 ID。

操作 Dock、任务栏、无法验证窗口归属的菜单或无窗口桌面时，使用 `computer_observe({"desktop":true})`。该模式返回首个显示器截图，不包含窗口或语义元素。可根据 `computer_status` 的桌面坐标设置 `region`，选择其他显示器或跨显示器截图。桌面快照坐标输入检查显示器布局，不要求窗口焦点。窗口列表覆盖可见、非最小化的应用窗口。

`computer_wait` 接受精确 `window_id`、可选 `text` 和 `timeout_ms`（默认 5,000，最多 10,000）。条件等待超时返回 `matched:false`；请求取消和原生错误仍返回错误。文本按区分大小写的子串匹配无障碍名称和非密码值，不执行 OCR。

激活会等待窗口焦点或非键盘焦点面板的鼠标操作条件确认，默认 2,000 毫秒，可通过 `timeout_ms` 设置，最多 10,000。确认超时返回 `activation_failed`，已识别的模态阻塞返回 `window_blocked`，原生激活错误直接返回。`computer_wait` 还支持 `active:true`、`focused:true` 和精确 `focus_name`，可与 `text` 组合；两个焦点条件均要求窗口已激活。键盘工具支持 `auto_activate:true` 和 `focus_name`，在发送前准备目标，共享默认 2,000 毫秒的准备预算，上限相同。基于快照的鼠标输入应在需要时激活目标，再观察后输入。

Windows 可能拒绝前台激活，此时返回 `activation_failed`，并附带目标与当前前台窗口 ID。请手动激活目标后重新观察。激活不会通过隐式按键或点击绕过限制。`window_blocked` 错误的 detail 返回模态阻塞窗口 ID，请单独观察并操作该窗口。

`computer_click`、`computer_scroll` 和 `computer_drag` 支持可选 `hold_keys` 数组，接受不重复的 `ctrl`、`alt`、`shift`、`meta` 和 `space`。按键在本次操作期间保持按下，结束、取消或失败后释放。这类组合操作要求窗口快照对应窗口具有 `active:true`，不支持桌面快照或只能接收鼠标的面板。具体效果由应用决定，例如 Shift 拖动可约束绘制，空格拖动可平移画布，Ctrl 滚动可缩放。对支持的画布，可在拖动请求中添加 `"hold_keys":["space"]` 执行平移。按键不会跨工具调用保持按下。

`computer_move` 接受 `snapshot_id`、图片坐标 `point` 和可选 `hover_ms`（0～3,000，默认 0）。它移动鼠标，不点击，在指定位置等待悬停时间后返回。坐标、窗口就绪和遮挡校验与其他坐标输入一致，操作消耗快照。之后重新观察以检查提示或菜单，例如 `computer_move({"snapshot_id":"<snapshot_id>","point":{"x":120,"y":80},"hover_ms":600})`。

激活窗口不会选择文本框。打开查找栏或对话框后，应检查观察结果中 `focused` 的元素。`focus_name` 是该控件的无障碍 `name`，控件无名称时省略。该参数等待焦点，不会转移焦点。键盘工具返回 `focus_timeout` 时，会附带最近观测的控件名称、角色和焦点状态，且未发送输入。

`computer_drag` 接受 `from` 和 `to`，或包含起终点的 `path`，共 2～128 个图片坐标点。整条路径为一次持续按下鼠标左键的笔画，各点之间以直线连接，可增加路径点近似曲线。`duration_ms` 为路径总时长，范围 100～3,000，默认 500。按下鼠标前检查全部路径点，取消或失败时释放鼠标。

```json
{
  "snapshot_id": "<snapshot_id>",
  "path": [
    { "x": 100, "y": 100 },
    { "x": 120, "y": 90 },
    { "x": 145, "y": 95 },
    { "x": 165, "y": 115 }
  ],
  "duration_ms": 800,
  "observe_after": { "screenshot": true }
}
```

观察支持区分大小写的 `text` 子串、忽略大小写精确匹配的原生 `role`、`region`、`compact:true` 和 `max_depth`（1～64，默认 64）。`compact` 省略无名称、无值且未聚焦的结构容器，继续遍历其后代。不截图时也可用 `region` 筛选相交的语义元素。通过 `root_id` 和当前 `snapshot_id` 可限定子树。

传入最新一次成功观察的 `since_snapshot_id` 可获取差异，要求该观察为窗口模式，且窗口、筛选条件、元素预算及 OCR 设置相同。输入消耗可操作引用后，该观察仍可作为差异基准。此时 `elements` 包含新增和变化节点，移除 `removed_element_ids` 后按 `element_order` 重建当前有界结果。`diff_complete:false` 表示至少一次观察发生截断，节点从结果消失不代表从应用消失。后续操作均须使用新 `snapshot_id`，未变化的元素也一样。原生身份变化表示为删除和新增。差异减少响应大小，不减少原生遍历工作。

macOS 可通过 `ocr:true` 对返回截图执行本地 Vision 识别，窗口观察需同时设置 `screenshot:true`。结果包含受 `max_elements` 限制的 `ocr` 文本区域、置信度和图片坐标，超出预算时标记 `ocr_truncated`。窗口捕获模式下，无名称且无 AX 值的控件可获得 `ocr_text`，原生名称和点击校验保持独立。屏幕截图中的文字可能来自覆盖窗口，因此以独立 OCR 区域返回。OCR 结果会省略与语义结果中密码控件相交的文本；其他可见敏感文本仍可能出现，尤其是纯截图或筛选观察。OCR 按需启用，会增加识别耗时，不上传图片。`computer_status.ocr` 表示支持状态，当前 npm Windows 分发版请求 OCR 会返回 `unsupported_operation`。

## 限制与行为

- 默认观察 200 个元素，最多 1,000 个。树遍历最多扫描 5,000 个节点，预算为三秒，未完成时返回 `truncated`。名称和值均有长度上限，原生 provider 阻塞可能超出遍历预算。
- 截图默认最大宽度 1,440，可设为 320～2,560，高度不超过 2,560。窗口观察默认 `capture_mode:"window"`，macOS 使用 ScreenCaptureKit，Windows 使用 PrintWindow。`capture_mode:"screen"` 采集可见桌面区域及覆盖窗口，桌面模式固定使用 screen。Windows 应用需支持 PrintWindow 渲染，不会自动降级为屏幕截图。截图以 MCP 图片内容返回，不写入磁盘。窗口源缓冲区及 Windows 屏幕区域最多 4,000 万像素。
- 窗口鼠标输入检查点击、移动、滚动位置及所有拖动路径点实际所属窗口，不逐像素检查路径点之间的线段。窗口截图可看到被遮挡内容，但不允许穿透覆盖窗口点击，检查失败返回 `target_obscured`。macOS 错误在可确认时返回命中角色和窗口 ID，并说明命中结果或归属何时无法验证。检查仍可能与外部变化发生竞争。
- 最新快照可用于操作，观察数据同时保留为下一次差异基准。原生引用受元素数量限制。单个固定线程执行桌面请求，最多排队 16 项。总请求预算为 15 秒，包括排队时间。
- 用户级文件锁协调本服务的多个实例，不能阻止人或其他软件切换焦点。`busy` 表示本进程队列已满或其他实例持有锁。
- 点击使用真实鼠标事件。组合键使用虚拟键；macOS 字母与数字快捷键对应 US 键盘物理位置。Unicode 文本插入不依赖这些快捷键。自绘控件可能需要图片坐标和具备视觉能力的 Agent。
- 工具成功表示输入已发送或系统调用已接受，不保证应用完成预期任务。超时和取消不会触发输入自动重试。
- 观察和等待超时会明确说明本次未发送输入。macOS 上，同一可执行路径存在多个存活进程时，ScreenCaptureKit 可能一直等待图像回调。若持续发生，请停止其他 MCP 实例，再重启受影响的实例。超时的原生操作在返回前仍占用工作线程。
- 语义快照省略密码值，截图仍可能包含屏幕上的敏感内容。Agent 应将桌面文本视为不可信输入。

本地构建、验证范围与 npm 发布准备见[开发与发布](docs/development.zh-CN.md)。贡献约定见 [AGENTS.zh-CN.md](AGENTS.zh-CN.md)。

[MIT](LICENSE) © Sanjeever。
