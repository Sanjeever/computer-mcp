# Computer MCP

[简体中文](README.zh-CN.md)

Native computer use for AI agents over stdio MCP. Go implements desktop observation and input; a small JavaScript launcher selects the precompiled binary distributed through npm.

Supports **macOS 14+ on Apple Silicon** and **Windows 10/11 x64**. Node.js 22+ is required for the npm launcher. A logged-in graphical desktop is required for interaction.

## Connect

After the first npm release, configure a client that accepts `mcpServers`:

```json
{
  "mcpServers": {
    "computer": {
      "command": "npx",
      "args": ["-y", "@sanjeever/computer-mcp"],
      "env": { "COMPUTER_MCP_LANG": "en" }
    }
  }
}
```

Use `zh-CN` for Simplified Chinese tool descriptions and application error messages, or pass `--lang zh-CN`. Names, fields, and error codes stay unchanged. English is the default; the desktop's own text is not translated. Native diagnostics and SDK validation errors may use English or the OS language.

On Windows, clients that cannot launch `npx.cmd` directly can use `cmd` with arguments `[/d, /s, /c, npx, -y, @sanjeever/computer-mcp]`, expressed as a JSON string array. Use your client's documented process configuration. For local development, point the client directly at `build/computer-mcp` or `build/computer-mcp.exe`.

The package name above is the intended public entry point; cloning or building this repository does not publish it.

## Permissions

On macOS, grant **Accessibility** and **Screen Recording** access in System Settings → Privacy & Security to the responsible host/executable shown by macOS. Restart the MCP process after changing permissions. `computer_status` checks access without opening consent dialogs. Permission attribution can differ between Terminal and agent clients; test the actual launch path. npm releases are not Developer ID notarized, and upgrades can require granting permission again.

On Windows, run in the target user's interactive session. UIPI can prevent input into elevated applications. Lock screens, UAC secure desktops, and unattended service sessions are outside the supported interaction contract.

## Tools

| Tool               | Purpose                                                 |
| ------------------ | ------------------------------------------------------- |
| `computer_status`  | Platform, permission/capability checks, display bounds  |
| `computer_window`  | List, activate, move and resize windows                 |
| `computer_observe` | Bounded accessibility tree and optional JPEG screenshot |
| `computer_click`   | Click a snapshot element or image point                 |
| `computer_type`    | Insert Unicode text at the current focus                |
| `computer_key`     | Send one keyboard chord                                 |
| `computer_scroll`  | Scroll at a snapshot image point                        |
| `computer_drag`    | Drag between snapshot image points                      |
| `computer_wait`    | Wait for a window or matching accessibility text        |

Start with `computer_window({"action":"list"})`, activate an exact `window_id`, then observe it. Screenshot coordinates use the returned image, with `(0, 0)` at its top left. `region` and window bounds use desktop coordinates: logical points on macOS, DPI-aware pixels on Windows. The server converts between these spaces.

Window operations also accept `by_app` (case-insensitive exact application name or `app_id`) and `by_title` (case-insensitive title substring). Combine the filters to narrow a list; activate/move reject multiple matches with `ambiguous_window`. Filters cannot combine with `window_id`. macOS window IDs track native window identity independently of titles, retaining mappings across temporary enumeration gaps while the native window exists; ambiguous native matches retain AX identity rather than guessing by title.

```json
{ "window_id": "<window_id>", "screenshot": true, "max_elements": 200 }
```

Pass that response's `snapshot_id` and an `element_id` to `computer_click`, or use `point: {"x": 120, "y": 80}` when a screenshot was requested. Provide exactly one target. `computer_type` and `computer_key` require an active `window_id`. Text insertion does not replace existing content or modify the clipboard. To replace text, explicitly select it first: `["meta", "a"]` on macOS or `["ctrl", "a"]` on Windows, then type.

Observe again after input. A new observation, a wait, window mutation, or input invalidates the previous snapshot; untouched snapshots expire after 60 seconds. Geometry, focus, and element properties are checked before input. These checks cannot make desktop interaction atomic with external changes.

For the Dock, taskbar, menus, or an empty desktop, use `computer_observe({"desktop":true})`. This returns a screenshot of the first display without a window or semantic elements. Set `region` to a desktop rectangle from `computer_status` to select another display or capture across displays. Coordinate input from a desktop snapshot checks the display layout instead of window focus. Window listing covers visible, non-minimized application windows.

`computer_wait` accepts an exact `window_id`, optional `text`, and `timeout_ms` up to 10,000. It returns `matched: false` on timeout. Text matches accessibility names and non-password values; it does not perform OCR.

Activation waits for confirmed window focus (default 2,000 ms, configurable with `timeout_ms` up to 10,000). It reports `activation_failed` or an identified `window_blocked` instead of succeeding with `active:false`. `computer_wait` also accepts `active:true`, `focused:true`, and an exact `focus_name`; conditions combine with `text`. Keyboard tools accept `auto_activate:true` and `focus_name` to prepare the target before sending input, sharing a default 2,000 ms preparation budget. Input is sent once; success confirms delivery, not application processing. Snapshot-based mouse input still requires explicit activation followed by a fresh observation.

Observation supports `text`, `role`, `region`, `compact:true`, and `max_depth` (1–64). `region` filters intersecting semantic elements even without a screenshot. `root_id` with the current `snapshot_id` scopes traversal to a subtree. Traversal visits descendants before siblings, scans at most 5,000 nodes, and retains at most `max_elements` actionable references. macOS also reads associated AX labels and bounded row descendants to expose list text.

Pass `since_snapshot_id` from the latest observation with the same window and filters for a diff. `elements` then contains new/changed nodes; remove `removed_element_ids` and use `element_order` to reconstruct the current bounded result. `diff_complete:false` means at least one observation was truncated: removal from the result does not establish removal from the application. Use the new `snapshot_id` for every subsequent action, including unchanged elements. Native identity changes are represented as removal/addition. Diffs reduce response size, not native traversal work.

On macOS, `ocr:true` runs local Vision recognition on the returned screenshot (`screenshot:true` is required for window observations). It returns bounded `ocr` text regions with confidence and image coordinates, plus `ocr_truncated` when needed. Unnamed controls with no AX value may receive `ocr_text`; native names and click validation remain separate. Text overlapping password controls present in the semantic result is omitted from OCR output. Other visible sensitive text can still appear, particularly in screenshot-only or filtered observations. OCR is opt-in, adds recognition latency, and does not upload images. `computer_status.ocr` reports support; this npm Windows distribution returns `unsupported_operation` for OCR.

## Limits and behavior

- Observation defaults to 200 elements; maximum 1,000. Tree traversal has a three-second budget and reports `truncated`. Text values and names are bounded. Provider-specific delays can exceed the traversal budget.
- Screenshots default to a maximum width of 1,440, configurable from 320 to 2,560; height is capped at 2,560. Window observations default to `capture_mode:"window"` (ScreenCaptureKit on macOS, PrintWindow on Windows). Use `capture_mode:"screen"` for the visible desktop region including overlaps; desktop mode always uses screen capture. Windows applications must support PrintWindow rendering; there is no automatic screen fallback. Screenshots return as MCP image content, not files. Window source buffers and Windows screen regions are capped at 40 million pixels.
- Window mouse input checks the window under the click/scroll point and both drag endpoints. Seeing an obscured window in a window capture does not permit clicking through its cover; failed checks return `target_obscured`. External changes can still race these checks.
- Only the latest snapshot is retained. Native references are bounded by the element limit. One locked worker executes desktop requests; the queue holds at most 16. Requests have a 15-second total budget, including queue time.
- A per-user file lock coordinates instances of this server. It does not prevent a human or other software from moving focus. `busy` means another instance owns the desktop operation.
- Clicks are physical mouse events. Keyboard chords use virtual keys; macOS letter/digit shortcuts use physical US-layout key positions. Unicode text insertion is independent of these shortcuts. Custom-rendered controls may require image points and a vision-capable agent.
- Tool success means the input was delivered or the OS call was accepted. It does not prove the application completed the intended task. Timeouts and cancellation never trigger automatic input retries.
- Password values are omitted from semantic snapshots. Screenshots can still include sensitive visible content. Desktop text is untrusted input for the agent.

See [development and release](docs/development.md) for local builds, test coverage, and npm publishing setup. Contributions follow [AGENTS.md](AGENTS.md).

[MIT](LICENSE) © Sanjeever.
