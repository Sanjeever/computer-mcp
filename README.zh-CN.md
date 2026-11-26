# Computer MCP

[English](https://github.com/Sanjeever/computer-mcp/blob/main/README.md)

Computer MCP 通过 stdio MCP 为 AI Agent 提供原生桌面操作。Agent 可用无障碍树和截图观察窗口，再点击、输入、滚动或拖拽。

## 用 Computer MCP 绘图

|                                                                                                                                 Windows 10 · 画图                                                                                                                                 |                                                                                                                                  macOS · 无边记                                                                                                                                   |
| :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: |
| [<img src="https://raw.githubusercontent.com/Sanjeever/computer-mcp/main/docs/images/saturn-ringhaven-poster.png" alt="环港复古旅行海报，画着土星环和环帆号" width="210">](https://raw.githubusercontent.com/Sanjeever/computer-mcp/main/docs/images/saturn-ringhaven-poster.png) | [<img src="https://raw.githubusercontent.com/Sanjeever/computer-mcp/main/docs/images/freeform-postcard.png" alt="明信片「愿你一路有光」，画着海边的灯塔和橙色月亮" width="435">](https://raw.githubusercontent.com/Sanjeever/computer-mcp/main/docs/images/freeform-postcard.png) |
|                                                                             [Claude Code · 环港海报](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md#用画图绘制土星旅行海报)                                                                              |                                                                                [Codex · 灯塔明信片](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md#用无边记画一张明信片)                                                                                 |

## 接入

需要 **Node.js 22+**，支持 **macOS 14+（Apple Silicon）** 和 **Windows 10/11（x64）**。桌面交互需要用户已登录图形会话。

### Codex

在 `~/.codex/config.toml` 中添加以下配置。

```toml
[mcp_servers.computer]
command = "npx"
args = ["-y", "@sanjeever/computer-mcp"]

[mcp_servers.computer.env]
COMPUTER_MCP_LANG = "zh-CN"
```

使用指南提供 Windows 的 `cmd.exe` 启动方式、语言设置及直接启动原生程序的配置。[客户端配置](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md#接入)

### 其他 MCP 客户端

在支持 `mcpServers` 的客户端添加以下配置。

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

## 权限

macOS 在系统设置中为显示的宿主或可执行程序授予**辅助功能**和**屏幕录制**权限，再重启 MCP。Windows 应使用目标用户的交互式会话。系统可能阻止向以管理员权限运行的应用发送输入。[权限说明](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md#权限)

## 工具

调用 `computer_status` 检查权限与能力。通过 `computer_window` 列出窗口并激活指定 `window_id`，再调用 `computer_observe`。鼠标操作使用最新快照及其对应的图片坐标。[工具说明](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md#工具)

[使用指南](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.zh-CN.md) · [开发与发布](https://github.com/Sanjeever/computer-mcp/blob/main/docs/development.zh-CN.md) · [版本发布](https://github.com/Sanjeever/computer-mcp/releases) · [MIT 许可证](https://github.com/Sanjeever/computer-mcp/blob/main/LICENSE)
