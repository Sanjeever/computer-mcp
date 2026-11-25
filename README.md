# Computer MCP

[简体中文](https://github.com/Sanjeever/computer-mcp/blob/main/README.zh-CN.md)

Computer MCP gives AI agents native desktop access over stdio MCP. Agents can observe windows through accessibility trees and screenshots, then click, type, scroll, or drag.

Requires **Node.js 22+** and a logged-in graphical session on **macOS 14+ (Apple Silicon)** or **Windows 10/11 (x64)**.

## Connect

Add this configuration to a client that accepts `mcpServers`.

```json
{
  "mcpServers": {
    "computer": {
      "command": "npx",
      "args": ["-y", "@sanjeever/computer-mcp"]
    }
  }
}
```

See the user guide for `cmd` on Windows, language settings, and direct native launch. [Client configuration](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.md#connect)

## Permissions

On macOS, grant the host or executable shown by System Settings **Accessibility** and **Screen Recording** access, then restart the MCP process. On Windows, use the target user's interactive session. Windows may block input to elevated apps. [Permission details](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.md#permissions)

## Tools

Call `computer_status` to check access. Use `computer_window` to list windows and activate an exact `window_id`, then call `computer_observe`. Use the newest snapshot and its own image coordinates for pointer input. [Tool reference](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.md#tools)

## Drawing a postcard in Freeform

Codex drew this postcard in macOS Freeform using Computer MCP. [Prompt and example](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.md#drawing-a-postcard-in-freeform)

![A lighthouse and orange moon over the sea on a postcard captioned “May you have light along the way”](https://raw.githubusercontent.com/Sanjeever/computer-mcp/main/docs/images/freeform-postcard.png)

[User guide](https://github.com/Sanjeever/computer-mcp/blob/main/docs/usage.md) · [Development and release](https://github.com/Sanjeever/computer-mcp/blob/main/docs/development.md) · [Releases](https://github.com/Sanjeever/computer-mcp/releases) · [MIT license](https://github.com/Sanjeever/computer-mcp/blob/main/LICENSE)
