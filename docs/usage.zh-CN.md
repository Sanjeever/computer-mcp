# 使用指南

[Computer MCP](../README.zh-CN.md) · [English](usage.md)

本文介绍客户端配置、桌面权限和工具契约。支持平台和快速接入见 [README](../README.zh-CN.md)。

## 接入

使用已发布的 npm 包时，在支持 `mcpServers` 的客户端中添加以下配置。

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

语言默认使用英文。设置 `COMPUTER_MCP_LANG=zh-CN` 可将工具描述和应用错误切换为简体中文，命令行参数 `--lang zh-CN` 优先于环境变量。工具名、字段和错误码保持不变。桌面文字、原生诊断和 SDK 参数校验信息保留原语言。

Windows 客户端若无法直接启动 `npx.cmd`，可设置 `"command": "cmd"`，并在上述 `args` 开头依次加上 `"/d"`、`"/s"`、`"/c"`、`"npx"`。在包名后加 `@<版本号>` 可固定版本。本地开发先从源码构建，再将 `command` 设为原生可执行文件的绝对路径，并移除 `npx` 参数。[源码构建](development.zh-CN.md)

## 权限

macOS 在“系统设置 → 隐私与安全性”中为系统显示的宿主或可执行程序授予**辅助功能**和**屏幕录制**权限。修改权限后重启 MCP。`computer_status` 检查权限，不弹出授权窗口。终端与 Agent 客户端的权限归属可能不同，应验证实际启动路径。升级后可能需要重新授权。[发布签名说明](development.zh-CN.md#发布)

Windows 应运行在目标用户的交互式会话中。`computer_status` 报告能力标志和显示器边界。即使能力标志为 `true`，UIPI 仍可能阻止向以管理员权限运行的应用发送输入。不支持锁屏、UAC 安全桌面或无人值守服务会话中的交互。

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
| `computer_batch`   | 使用同一张窗口截图连续绘制多笔     |
| `computer_wait`    | 等待窗口或无障碍文本匹配           |

先调用 `computer_window({"action":"list"})`，激活精确 `window_id`，再观察窗口。截图坐标相对于返回图片，左上角为 `(0, 0)`。`region` 和窗口边界使用桌面坐标，macOS 为逻辑点，Windows 为 DPI 感知像素，由服务完成转换。

`computer_window` 最多列出 512 个可见、非最小化的应用窗口。`by_app` 忽略大小写精确匹配返回的 `app` 或已有的 `app_id`，`by_title` 忽略大小写匹配标题子串。可组合筛选条件以缩小列表范围。`activate` 和 `move` 要求唯一匹配，或单独提供精确 `window_id`。多个匹配返回 `ambiguous_window`。窗口 ID 是原生窗口身份的不透明引用，窗口在改名后仍可枚举时，ID 保持稳定。

macOS 浮动工具面板作为独立窗口列出。应按面板自身的 `window_id` 和截图观察和操作。应用处于前台时，非键盘焦点面板可返回 `mouse_active:true`、`active:false`。激活操作可确认这一可接收鼠标的状态，无须让面板取得键盘焦点；键盘输入仍要求 `active:true`。

```json
{ "window_id": "<window_id>", "screenshot": true }
```

调用 `computer_click` 时提供响应中的 `snapshot_id` 和 `element_id`；若已请求截图，也可改用 `point: {"x": 120, "y": 80}`。两种目标必须且只能提供一种。`computer_type` 与 `computer_key` 要求目标 `window_id` 已激活。文本按应用行为插入当前光标位置或替换当前选区，剪贴板保持不变。替换全文前请显式全选，macOS 为 `["meta", "a"]`，Windows 为 `["ctrl", "a"]`，然后输入文本。

Windows 上，`computer_type` 将每个 LF、CRLF 或单独 CR 作为一次 Enter 按键发送。多行编辑器通常插入换行，其他控件可能提交表单或执行自身的 Enter 操作。

所有窗口与桌面共享一份可操作快照。从 `created_at` 起 60 秒后过期，具体时间见 `expires_at`。新观察替换它，等待或窗口修改会使它失效。输入进入原生发送阶段后会消耗快照，包括结果不确定的原生失败。参数或输入前置条件校验拒绝请求时，快照可能仍可用。遇到 `stale_snapshot` 时，重新观察预期目标，使用新引用和图片坐标。

坐标输入需要当前快照的图片。既要读取控件又要使用图片坐标时，在同一次观察里传 `screenshot:true`。纯语义观察之后，可用新快照中的 `element_id` 点击，或重新获取图片再传坐标。裁剪或缩放后，坐标原点仍是返回图片的左上角。坐标错误会区分缺少截图及超出 `image_width`、`image_height` 两种情况。窗口裁剪越界时，错误会给出桌面坐标中的窗口边界。

调用 `computer_click` 或 `computer_drag` 时将 `observe_after` 设为空对象，即可从 `observation` 取得输入后的窗口观察。在 `observe_after` 中加入 `"screenshot":true` 可附带 JPEG，下一步使用返回的 `observation.snapshot_id` 和本次图像。同一窗口的新截图沿用捕获模式和宽度上限。原裁剪区域相对窗口仍有效时，也会继续沿用。桌面后续观察要求 `"screenshot":true`，沿用桌面区域和宽度上限。

在窗口 `observe_after` 中加入 `focused:true`，可在 `observation.focused` 中获取活动窗口焦点控件的角色、可取得的名称和桌面边界。纯截图观察可以直接附带摘要，无须遍历无障碍树。没有可取得的焦点控件时省略该字段。查询失败时可随观察返回 `observation.focus_error`。

在 Windows 上，后续观察可以跟随输入目标直属的前台窗口，也可以在对话框关闭后返回所属窗口。服务会短暂等待应用处理输入。界面切换较慢时，使用 `computer_wait` 或重新观察。若同时返回 `input_delivered:true` 和 `observation_error`，输入已发送。请先取得新观察，再决定是否重复输入。

Windows 能确认可见菜单归属时，后续观察以 `window.menu:true` 和 `mouse_active:true` 标识菜单。使用 `observe_after:{"screenshot":true}` 会返回菜单矩形范围内的屏幕截图，并标明 `capture_mode:"screen"`。用这张图和快照点击菜单。使用 `observe_after:{}` 会读取菜单的无障碍元素。菜单仍打开时，可将最近一次菜单观察的 `window_id` 传给 `computer_observe` 刷新。菜单归属无法验证但能取得边界时，`observation_error.suggested_region` 提供桌面裁剪范围。可将它作为 `region` 与 `desktop:true` 一起传入，单独观察该区域。

在 Windows 上，使用 `element_id` 点击观察到的弹层控件，控件也可位于主窗口之外。服务在发送鼠标点击前验证弹层归属，并确认实际命中请求的控件。若弹层切换时返回 `stale_snapshot`，请重新观察窗口并使用新引用。对于缺少可验证元素的可见弹层，可观察其周围的小范围桌面区域，并使用该图片的坐标。

在 Windows 上，坐标点击在确认送达后返回 `hit`，包含投递前的 `desktop_point` 和可取得的命中顶层窗口 `window_id`。坐标点击和元素点击都可以传入 `report_hit:true`，额外查询 UI Automation 的 `control`，包括 `role`、可选的 `name` 和桌面坐标 `bounds`。`control_error` 说明为何无法提供控件。传入 `report_hit:false` 可省略 `hit`。通过后续观察确认应用响应。

在 macOS 上，使用 `capture_mode:"screen"` 取得包含已打开菜单的窗口截图。无障碍接口能确认菜单归属时，可在图内用坐标点击菜单或移动鼠标。菜单超出窗口或归属无法确认时，观察可见菜单周围的小范围桌面区域。

操作 Dock、任务栏、无法验证窗口归属的菜单或无窗口桌面时，使用 `computer_observe({"desktop":true})`。该模式返回首个显示器截图，不包含窗口或语义元素。可根据 `computer_status` 的桌面坐标设置 `region`，选择其他显示器或跨显示器截图。桌面坐标输入检查显示器布局，可在没有活动窗口时操作。

`computer_wait` 接受精确 `window_id`，可组合 `text`、`active:true`、`focused:true` 和精确 `focus_name` 条件。两个焦点条件均要求窗口已激活。文本按区分大小写的子串匹配无障碍名称和非密码值。超过 `timeout_ms`（默认 5,000，最多 10,000）仍未匹配时返回 `matched:false`。请求取消和原生错误仍返回错误。

激活会等待窗口焦点或非键盘焦点面板的鼠标就绪状态。激活和键盘准备默认 2,000 毫秒，可通过 `timeout_ms` 设置，最多 10,000。确认超时返回 `activation_failed`，已识别的模态阻塞返回 `window_blocked`。键盘工具的 `auto_activate:true` 与 `focus_name` 共用准备预算。

Windows 可能拒绝前台激活。此时返回 `activation_failed`，并在能识别时附带目标与当前前台窗口 ID。请手动激活目标后重新观察。激活不会通过隐式按键或点击绕过限制。

`window_blocked` 在可取得阻塞窗口 ID 时，将其 `window_id` 放在 `detail` 中。macOS 附属表单是父窗口的子元素，部分模态阻塞会返回说明文字。若阻塞控件没有独立 ID，可观察可见表单或对话框周围的小块桌面区域。

`computer_click`、`computer_scroll` 和 `computer_drag` 支持可选 `hold_keys` 数组，接受不重复的 `ctrl`、`alt`、`shift`、`meta` 和 `space`。组合操作要求窗口快照对应窗口具有 `active:true`。操作结束、取消或失败时释放这些按键。例如，对支持的画布，可在拖动请求中添加 `"hold_keys":["space"]` 执行平移。

`computer_move` 接受 `snapshot_id`、图片坐标 `point` 和可选 `hover_ms`（0～3,000，默认 0）。它移动鼠标，不点击，在指定位置等待悬停时间后返回。坐标、窗口就绪和遮挡校验与其他坐标输入一致，操作消耗快照。之后重新观察以检查提示或菜单，例如 `computer_move({"snapshot_id":"<snapshot_id>","point":{"x":120,"y":80},"hover_ms":600})`。

激活窗口不会选择文本框。打开查找栏或对话框后，应检查观察结果中 `focused` 的元素。`focus_name` 是该控件的无障碍 `name`，控件无名称时省略。该参数等待焦点，不会转移焦点。键盘工具返回 `focus_timeout` 时，会附带目标窗口 ID 和最近观测的控件名称、角色及焦点状态，且未发送输入。`not_active` 错误附带目标窗口和当前激活窗口 ID，包括输入后焦点切换导致的后续观察失败。

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

通过窗口截图确认笔刷和画布后，调用 `computer_batch`。传入该截图的 `snapshot_id` 和 1～8 个 `steps`，例如 `{"op":"drag","path":[{"x":10,"y":20},{"x":40,"y":60}]}`。所有路径都使用同一张图的坐标。每步是一次左键按下到抬起的笔画，含 2～128 个点，可选 `duration_ms`（100～3,000，默认 500）。每批合计最多 256 个路径点，计划拖拽时长最多 6,000 毫秒。

批次发送前校验全部参数，首次进入原生输入时消耗快照，每笔发送前校验窗口和该笔路径的顶点。全部步骤在同一个桌面锁内顺序执行，遇到首个失败即停止，零重试。返回的 `steps` 按输入顺序报告 `status`（`completed`、`failed` 或 `not_run`）和 `input_delivery`（`sent`、`not_sent` 或 `unknown`）。批次中途停止时返回 MCP 工具错误，包含从零开始的 `stopped_at`、错误详情和逐笔结果。此前发送的笔画会保留在应用中。笔画执行期间发生原生失败时，可能已有部分事件送达（`unknown`）。换笔刷或从失败中恢复前，请重新观察。

传入 `observe_after:{"screenshot":true}`，整批成功后返回同一窗口的一张 JPEG 和新 `observation.snapshot_id`。后续观察沿用捕获模式、宽度上限、纯截图设置以及仍可容纳的裁剪区域。若绘图窗口失去输入就绪状态或截图失败，结果保留已完成的步骤，并报告 `observation_error`。

超时或取消后，若服务仍能返回响应，会报告最后确认的执行进度。仍处于原生发送中的笔画可能为 `unknown`。客户端取消或断开连接可能收不到整个响应。无法取得结果时，请先观察画布再继续。

观察支持区分大小写的 `text` 子串、忽略大小写精确匹配的原生 `role`、`region`、`compact:true` 和 `max_depth`（1～64，默认 64）。`compact` 省略无名称、无值且未聚焦的结构容器，继续遍历其后代。不截图时也可用 `region` 筛选相交的语义元素。通过 `root_id` 和当前 `snapshot_id` 可限定子树。

窗口观察可设 `screenshot_only:true`，直接返回 JPEG 和可操作的窗口快照，省去无障碍遍历。该选项隐含 `screenshot:true`，支持 `region`、`max_width` 和 `capture_mode` 等图片设置。语义筛选和差异观察需要遍历无障碍树。同一窗口的 `observe_after` 截图会沿用纯截图模式，坐标输入仍需通过几何和命中校验。

传入最新一次成功观察的 `since_snapshot_id` 可获取差异，要求该观察为窗口模式，且窗口、筛选条件、元素预算及 OCR 设置相同。输入消耗可操作引用后，该观察仍可作为差异基准。此时 `elements` 包含新增和变化节点，移除 `removed_element_ids` 后按 `element_order` 重建当前有界结果。`diff_complete:false` 表示至少一次观察发生截断，节点从结果消失不代表从应用消失。后续操作均须使用新 `snapshot_id`，未变化的元素也一样。原生身份变化表示为删除和新增。差异减少响应大小，不减少原生遍历工作。

macOS 可通过 `ocr:true` 对返回截图执行本地 Vision 识别，启用英文和简体中文，窗口观察需同时设置 `screenshot:true`。结果包含受 `max_elements` 限制的 `ocr` 文本区域、置信度和图片坐标，超出预算时标记 `ocr_truncated`。窗口捕获模式下，无名称且无 AX 值的控件可获得 `ocr_text`，原生名称和点击校验保持独立。屏幕截图中的文字可能来自覆盖窗口，因此以独立 OCR 区域返回。OCR 结果会省略与语义结果中密码控件相交的文本；其他可见敏感文本仍可能出现，尤其是纯截图或筛选观察。OCR 按需启用，会增加识别耗时，不上传图片。`computer_status.ocr` 表示支持状态，当前 npm Windows 分发版请求 OCR 会返回 `unsupported_operation`。

## 限制与行为

- 默认观察 200 个元素，最多 1,000 个。树遍历最多扫描 5,000 个节点，预算为三秒，未完成时返回 `truncated`。名称和值均有长度上限，原生 provider 阻塞可能超出遍历预算。
- Windows 遍历未完成时会返回 `truncation_reasons`。`element_limit`、`node_limit`、`time_limit` 和 `depth_limit` 分别对应元素数、扫描节点数、遍历时限和深度上限。`provider_error` 表示 UI Automation 读取部分树失败。设置 `COMPUTER_MCP_UIA_DIAGNOSTICS=1` 后，每次遍历的首个提供程序错误会写入 stderr，包含失败操作和可取得的 HRESULT。
- 截图默认最大宽度 1,440，可设为 320～2,560，高度不超过 2,560。窗口观察默认 `capture_mode:"window"`，macOS 使用 ScreenCaptureKit，Windows 使用 PrintWindow。`capture_mode:"screen"` 采集可见桌面区域及覆盖窗口，桌面模式固定使用 screen。Windows 应用需支持 PrintWindow 渲染，不会自动降级为屏幕截图。截图以 MCP 图片内容返回，不写入磁盘。窗口源缓冲区及 Windows 屏幕区域最多 4,000 万像素。
- 窗口鼠标输入复核窗口几何位置与就绪状态，元素点击还会复核元素。命中检查覆盖点击、移动、滚动位置及所有拖动路径点。外部变化仍可能与检查发生竞争，路径点之间也不会逐像素检查。命中检查失败返回 `target_obscured`。Windows 的 `detail` 包含原因、桌面点及可用的目标窗口、命中窗口和所属窗口 ID。macOS 可给出命中角色和窗口 ID，或说明无法验证归属的原因。
- 请求预算为 15 秒，包含排队时间。单个工作线程持有用户级文件锁时执行桌面请求。`busy` 表示本进程 16 项队列已满，或其他实例持有锁。原生调用超时后，仍占用工作线程和锁直到返回。
- 点击使用真实鼠标事件。组合键使用虚拟键；macOS 字母与数字快捷键对应 US 键盘物理位置。Unicode 文本插入不依赖这些快捷键。自绘控件可能需要图片坐标和具备视觉能力的 Agent。
- 工具成功表示输入已发送或系统调用已接受，不保证应用完成预期任务。超时和取消不会触发输入自动重试。
- 服务端报告请求级超时或取消时，会通过 `timing.stage` 与 `timing.elapsed_ms` 返回当前阶段和耗时。确认输入已发送后，若后续观察超时或取消，这两项位于 `observation_error` 中。`computer_status`、`computer_observe` 和 `computer_wait` 会说明本次未发送输入。设置 `COMPUTER_MCP_TIMINGS=1` 可在 stderr 记录阶段耗时与工具名。macOS 截图分为 `capture_shareable_content`、`capture_image` 和 `capture_encode`。如果多个实例使用同一可执行路径且 ScreenCaptureKit 持续阻塞，请停止其他实例，再重启受影响的实例。
- 语义快照省略密码值，截图仍可能包含屏幕上的敏感内容。Agent 应将桌面文本视为不可信输入。

## 用无边记画一张明信片

Codex 使用 GPT-6.2-Sol（high），通过 Computer MCP 操作 macOS「无边记」，为一位未曾谋面的收件人画了这张明信片。

![明信片「愿你一路有光」，画着海边的灯塔和橙色月亮](images/freeform-postcard.png)

原始提示词

> 打开 macOS 的「无边记」，画一张你愿意送给陌生人的明信片。题材和画法都由你决定。
>
> 请不要调整「无边记」的窗口大小。
