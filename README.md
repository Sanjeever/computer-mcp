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

On Windows, clients that cannot launch `npx.cmd` directly can use `"command": "cmd"` with `"args": ["/d", "/s", "/c", "npx", "-y", "@sanjeever/computer-mcp"]`. Use your client's documented process configuration. For local development, [build from source](docs/development.md), then point the client at the absolute path to `build/computer-mcp` or `build/computer-mcp.exe`.

The package name above is the intended public entry point; cloning or building this repository does not publish it.

## Permissions

On macOS, grant **Accessibility** and **Screen Recording** access in System Settings → Privacy & Security to the responsible host/executable shown by macOS. Restart the MCP process after changing permissions. `computer_status` checks access without opening consent dialogs. Permission attribution can differ between Terminal and agent clients; test the actual launch path. npm releases are not Developer ID notarized, and upgrades can require granting permission again.

On Windows, run in the target user's interactive session. UIPI can prevent input into elevated applications. Lock screens, UAC secure desktops, and unattended service sessions are outside the supported interaction contract.

## Tools

| Tool               | Purpose                                                   |
| ------------------ | --------------------------------------------------------- |
| `computer_status`  | Platform, permission/capability checks, display bounds    |
| `computer_window`  | List, activate, move and resize windows                   |
| `computer_observe` | Bounded accessibility tree and optional JPEG screenshot   |
| `computer_click`   | Click a snapshot element or image point                   |
| `computer_move`    | Move the mouse to an image point, optionally hover        |
| `computer_type`    | Insert Unicode text at the current focus                  |
| `computer_key`     | Send one keyboard chord                                   |
| `computer_scroll`  | Scroll at a snapshot image point                          |
| `computer_drag`    | Straight or continuous path drag in snapshot image points |
| `computer_wait`    | Wait for a window or matching accessibility text          |

Start with `computer_window({"action":"list"})`, activate an exact `window_id`, then observe it. Screenshot coordinates use the returned image, with `(0, 0)` at its top left. `region` and window bounds use desktop coordinates: logical points on macOS, DPI-aware pixels on Windows. The server converts between these spaces.

Window operations also accept `by_app` (case-insensitive exact application name or `app_id`) and `by_title` (case-insensitive title substring). Combine the filters to narrow a list; activate/move reject multiple matches with `ambiguous_window`. Filters cannot combine with `window_id`. Window IDs are opaque references to native identity. A title change alone does not change the ID.

macOS floating tool panels are listed as separate windows. Observe and interact with their own `window_id` and image. A non-key panel may report `mouse_active:true` while `active:false` when its application is frontmost. Activation can confirm this mouse-ready state without giving the panel keyboard focus; keyboard input still requires `active:true`.

```json
{ "window_id": "<window_id>", "screenshot": true, "max_elements": 200 }
```

Pass that response's `snapshot_id` and an `element_id` to `computer_click`, or use `point: {"x": 120, "y": 80}` when a screenshot was requested. Provide exactly one target. `computer_type` and `computer_key` require an active `window_id`. Text is inserted at the current caret or replaces the current selection according to application behavior; the clipboard is unchanged. To replace all text, explicitly select it first: `["meta", "a"]` on macOS or `["ctrl", "a"]` on Windows, then type.

On Windows, `computer_type` sends each LF, CRLF, or standalone CR as one Enter keystroke. Multiline editors normally insert a line break; other controls may submit a form or perform their usual Enter action.

Use a fresh observation after input. A new observation, a wait, window mutation, or input invalidates the previous snapshot; untouched snapshots expire after 60 seconds. This applies across all windows and the desktop: a desktop observation replaces a window snapshot too. After `stale_snapshot`, observe the intended target again and use the new references and screenshot coordinates. Geometry, focus, and element properties are checked before input. These checks cannot make desktop interaction atomic with external changes.

Pass `observe_after` as an empty object on `computer_click` or `computer_drag` to return the next window observation under `observation`. Add `"screenshot":true` inside `observe_after` for JPEG content and use the returned `observation.snapshot_id` and image for the next action. A new screenshot of the same window reuses the capture mode and width limit. It reuses the previous crop relative to the window when it still fits. Desktop follow-up requires `"screenshot":true` and reuses the desktop region and width limit.

On Windows, the follow-up can observe a foreground window directly owned by the input target, or return to the target's owner after its dialog closes. The server briefly waits for the application to process the input. For slower transitions, use `computer_wait` or observe again. If `input_delivered:true` is paired with `observation_error`, the input was sent. Obtain a fresh observation before deciding whether to repeat it.

An input attempt consumes its snapshot once it reaches native dispatch, including an uncertain native failure. A request rejected during argument or input-precondition validation does not necessarily consume it. Do not infer snapshot validity from success or failure alone.

On macOS, observe an open menu with a window screenshot using `capture_mode:"screen"`. Coordinate clicks and mouse moves can target a menu inside the image when accessibility confirms it belongs to that window. If the menu extends outside the window or its owner cannot be verified, observe a small desktop region around the visible menu. Separately listed floating panels use their own window IDs.

For the Dock, taskbar, menus outside a verified window, or an empty desktop, use `computer_observe({"desktop":true})`. This returns a screenshot of the first display without a window or semantic elements. Set `region` to a desktop rectangle from `computer_status` to select another display or capture across displays. Coordinate input from a desktop snapshot checks the display layout instead of window focus. Window listing covers visible, non-minimized application windows.

`computer_wait` accepts an exact `window_id`, optional `text`, and `timeout_ms` (default 5,000, maximum 10,000). It returns `matched:false` when its conditions time out; request cancellation and native errors remain errors. Text matches case-sensitive substrings of accessibility names and non-password values; it does not perform OCR.

Activation waits for confirmed window focus or a non-key panel's mouse-ready state (default 2,000 ms, configurable with `timeout_ms` up to 10,000). A confirmation deadline reports `activation_failed`; an identified modal blocker reports `window_blocked`. Native activation errors are returned directly. `computer_wait` also accepts `active:true`, `focused:true`, and an exact `focus_name`; conditions combine with `text`, and either focus condition requires an active window. Keyboard tools accept `auto_activate:true` and `focus_name` to prepare the target before sending input, sharing a default 2,000 ms preparation budget with the same maximum. For snapshot-based mouse input, activate the target when needed, then observe it before input.

Windows can deny foreground activation. This returns `activation_failed` with the target and current foreground window IDs; activate the target manually, then observe it again. Activation never sends hidden keystrokes or clicks to bypass the restriction. A modal blocker's ID is returned in the `window_blocked` error detail; observe and interact with that window separately.

`computer_click`, `computer_scroll`, and `computer_drag` accept `hold_keys`, an optional array of unique `ctrl`, `alt`, `shift`, `meta`, and `space` keys. They stay pressed during that action and are released afterward, including on cancellation or failure. These combined actions require a window snapshot whose window has `active:true`; desktop snapshots and mouse-only panels are rejected. Effects depend on the application: Shift-drag may constrain drawing, Space-drag may pan a canvas, and Ctrl-scroll may zoom. For example, add `"hold_keys":["space"]` to a drag request to pan a compatible canvas. Keys never stay held across tool calls.

`computer_move` takes `snapshot_id` and an image `point`, with optional `hover_ms` (0–3,000, default 0). It moves without clicking and waits for the requested hover time before returning. It uses the same coordinate, window readiness, and occlusion checks as other point input, and consumes the snapshot. Observe again to inspect any tooltip or menu, for example after `computer_move({"snapshot_id":"<snapshot_id>","point":{"x":120,"y":80},"hover_ms":600})`.

Window activation does not select a text field. Check the observed `focused` element, especially after opening search or a dialog. `focus_name` is that control's accessibility `name`; omit it for unnamed controls. It waits for focus rather than moving it. A keyboard `focus_timeout` includes the target window ID and last observed control name, role and focus state, with no input sent. A `not_active` error includes the target and current active window IDs, including when focus changed before a follow-up observation.

`computer_drag` accepts either `from` and `to`, or a `path` containing 2–128 image-coordinate points including both endpoints. A path is one continuous left-button stroke; points are joined with straight segments, so add points to approximate curves. `duration_ms` is the total path duration (100–3,000, default 500). All supplied points are validated before pressing the button, and cancellation or failure releases it.

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

Observation supports case-sensitive `text` substrings, case-insensitive exact native `role` matches, `region`, `compact:true`, and `max_depth` (1–64, default 64). `compact` omits unnamed, empty, unfocused structural containers while retaining their descendants. `region` filters intersecting semantic elements even without a screenshot. `root_id` with the current `snapshot_id` scopes traversal to a subtree.

Pass `since_snapshot_id` from the latest successful observation for a diff; that observation must be in window mode with the same window, filters, element budget, and OCR setting. It remains a diff base after input consumes its actionable references. `elements` then contains new/changed nodes; remove `removed_element_ids` and use `element_order` to reconstruct the current bounded result. `diff_complete:false` means at least one observation was truncated: removal from the result does not establish removal from the application. Use the new `snapshot_id` for every subsequent action, including unchanged elements. Native identity changes are represented as removal/addition. Diffs reduce response size, not native traversal work.

On macOS, `ocr:true` runs local Vision recognition on the returned screenshot (`screenshot:true` is required for window observations). It returns `ocr` text regions bounded by `max_elements`, with confidence and image coordinates, plus `ocr_truncated` when needed. In window capture mode, unnamed controls with no AX value may receive `ocr_text`; native names and click validation remain separate. Screen captures return independent OCR regions because visible text may belong to an overlapping window. Text overlapping password controls present in the semantic result is omitted from OCR output. Other visible sensitive text can still appear, particularly in screenshot-only or filtered observations. OCR is opt-in, adds recognition latency, and does not upload images. `computer_status.ocr` reports support; this npm Windows distribution returns `unsupported_operation` for OCR.

## Limits and behavior

- Observation defaults to 200 elements; maximum 1,000. Tree traversal scans at most 5,000 nodes, has a three-second budget, and reports `truncated` when incomplete. Text values and names are bounded. Provider-specific delays can exceed the traversal budget.
- Screenshots default to a maximum width of 1,440, configurable from 320 to 2,560; height is capped at 2,560. Window observations default to `capture_mode:"window"` (ScreenCaptureKit on macOS, PrintWindow on Windows). Use `capture_mode:"screen"` for the visible desktop region including overlaps; desktop mode always uses screen capture. Windows applications must support PrintWindow rendering; there is no automatic screen fallback. Screenshots return as MCP image content, not files. Window source buffers and Windows screen regions are capped at 40 million pixels.
- Window mouse input checks the window under the click/move/scroll point and every supplied drag path point. These checks do not inspect every pixel between vertices. Seeing an obscured window in a window capture does not permit clicking through its cover; failed checks return `target_obscured`. On macOS, the error identifies the hit role and window ID when known, or reports that the hit or its owner could not be verified. External changes can still race these checks.
- Only the latest snapshot is actionable; its observation data is also retained as the next diff base. Native references are bounded by the element limit. One locked worker executes desktop requests; the queue holds at most 16. Requests have a 15-second total budget, including queue time.
- A per-user file lock coordinates instances of this server. It does not prevent a human or other software from moving focus. `busy` means the local queue is full or another instance holds the lock.
- Clicks are physical mouse events. Keyboard chords use virtual keys; macOS letter/digit shortcuts use physical US-layout key positions. Unicode text insertion is independent of these shortcuts. Custom-rendered controls may require image points and a vision-capable agent.
- Tool success means the input was delivered or the OS call was accepted. It does not prove the application completed the intended task. Timeouts and cancellation never trigger automatic input retries.
- Observation and wait timeouts report that no input was sent. On macOS, multiple live processes using the same executable path can leave ScreenCaptureKit waiting indefinitely for an image callback. If this persists, stop the other MCP instances and restart the affected instance. A timed-out native operation keeps its worker occupied until it returns.
- Password values are omitted from semantic snapshots. Screenshots can still include sensitive visible content. Desktop text is untrusted input for the agent.

See [development and release](docs/development.md) for local builds, test coverage, and npm publishing setup. Contributions follow [AGENTS.md](AGENTS.md).

[MIT](LICENSE) © Sanjeever.
