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

```json
{ "window_id": "<window_id>", "screenshot": true, "max_elements": 200 }
```

调用 `computer_click` 时提供响应中的 `snapshot_id` 和 `element_id`；若已请求截图，也可改用 `point: {"x": 120, "y": 80}`。两种目标必须且只能提供一种。`computer_type` 与 `computer_key` 要求目标 `window_id` 已激活。文本插入不替换已有内容，不修改剪贴板。替换前请显式全选，macOS 为 `["meta", "a"]`，Windows 为 `["ctrl", "a"]`，然后输入文本。

输入后重新观察。新观察、等待、窗口修改或输入都会使旧快照失效；未使用的快照在 60 秒后过期。输入前检查几何位置、焦点和元素属性，这些检查不能消除与外部桌面变化之间的竞争窗口。

`computer_wait` 接受精确 `window_id`、可选 `text` 和不超过 10,000 的 `timeout_ms`。超时返回 `matched: false`。文本匹配无障碍名称和非密码值，不执行 OCR。

## 限制与行为

- 默认观察 200 个元素，最多 1,000 个。树遍历预算为三秒，未完成时返回 `truncated`。名称和值均有长度上限，原生 provider 阻塞可能超出遍历预算。
- 截图默认最大宽度 1,440，可设为 320～2,560，高度不超过 2,560。截图采集可见桌面区域，包括覆盖其上的其他窗口，以 MCP 图片内容返回，不写入磁盘。Windows 原始截图区域最多 4,000 万像素。
- 保留最新快照，原生引用受元素数量限制。单个固定线程执行桌面请求，最多排队 16 项。总请求预算为 15 秒，包括排队时间。
- 用户级文件锁协调本服务的多个实例，不能阻止人或其他软件切换焦点。`busy` 表示其他实例正在执行桌面操作。
- 点击使用真实鼠标事件。组合键使用虚拟键；macOS 字母与数字快捷键对应 US 键盘物理位置。Unicode 文本插入不依赖这些快捷键。自绘控件可能需要图片坐标和具备视觉能力的 Agent。
- 工具成功表示输入已发送或系统调用已接受，不保证应用完成预期任务。超时和取消不会触发输入自动重试。
- 语义快照省略密码值，截图仍可能包含屏幕上的敏感内容。Agent 应将桌面文本视为不可信输入。

本地构建、验证范围与 npm 发布准备见[开发与发布](docs/development.zh-CN.md)。贡献约定见 [AGENTS.md](AGENTS.md)。

[MIT](LICENSE) © Sanjeever。
