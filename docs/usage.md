# User guide

[Computer MCP](../README.md) · [简体中文](usage.zh-CN.md)

This guide covers client configuration, desktop permissions, and the tool contract. See the [README](../README.md) for supported platforms and a quick start.

## Connect

### Codex

Add this block to `~/.codex/config.toml`. Set `COMPUTER_MCP_LANG` under `[mcp_servers.computer.env]` if you want to use Simplified Chinese. [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)

```toml
[mcp_servers.computer]
command = "npx"
args = ["-y", "@sanjeever/computer-mcp"]
```

If Codex on Windows cannot launch `npx`, use this block instead.

```toml
[mcp_servers.computer]
command = "cmd.exe"
args = ["/d", "/s", "/c", "npx", "-y", "@sanjeever/computer-mcp"]
```

### Other MCP clients

To use the published npm package, configure a client that accepts `mcpServers`.

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

On Windows, clients that cannot launch `npx.cmd` directly can use `"command": "cmd"` and prefix the `args` above with `"/d", "/s", "/c", "npx"`.

### Language and launch options

English is the default. Set `COMPUTER_MCP_LANG` to `zh-CN` for Simplified Chinese tool descriptions and application errors. The command-line option `--lang zh-CN` overrides the environment setting. Tool names, fields, and error codes stay unchanged. Desktop text, native diagnostics, and SDK validation errors retain their original language.

Append `@<version>` to the package name to pin a release. To run a source build, set `command` to the absolute path to the native executable and remove the `npx` arguments. [Build from source](development.md)

## Permissions

On macOS, grant **Accessibility** and **Screen Recording** access in System Settings → Privacy & Security to the host or executable shown by macOS. Restart the MCP process after changing permissions. `computer_status` checks access without opening consent dialogs. Test the actual launch path because permission attribution can differ between Terminal and agent clients. Upgrades can require granting permission again. See [release signing](development.md#release).

On Windows, run in the target user's interactive session. `computer_status` reports capability flags and display bounds. UIPI can prevent input into elevated applications even when the flags are true. Lock screens, UAC secure desktops, and unattended service sessions are outside the supported interaction contract.

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
| `computer_batch`   | Draw consecutive strokes from one window screenshot       |
| `computer_wait`    | Wait for a window or matching accessibility text          |

Start with `computer_window({"action":"list"})`, activate an exact `window_id`, then observe it. Screenshot coordinates use the returned image, with `(0, 0)` at its top left. `region` and window bounds use desktop coordinates: logical points on macOS, DPI-aware pixels on Windows. The server converts between these spaces.

`computer_window` lists up to 512 visible, non-minimized application windows. Use the returned `app` or `app_id` when present for `by_app` (case-insensitive exact match) and a case-insensitive title substring for `by_title`. Combine these filters to narrow a list. For `activate` and `move`, select a unique match or supply an exact `window_id` by itself. Multiple matches return `ambiguous_window`. Window IDs are opaque references to native identity and survive title changes while the window remains discoverable.

macOS floating tool panels are listed as separate windows. Observe and interact with their own `window_id` and image. A non-key panel may report `mouse_active:true` while `active:false` when its application is frontmost. Activation can confirm this mouse-ready state without giving the panel keyboard focus; keyboard input still requires `active:true`.

```json
{ "window_id": "<window_id>", "screenshot": true }
```

Pass that response's `snapshot_id` and an `element_id` to `computer_click`, or use `point: {"x": 120, "y": 80}` when a screenshot was requested. Provide exactly one target. `computer_type` and `computer_key` require an active `window_id`. Text is inserted at the current caret or replaces the current selection according to application behavior; the clipboard is unchanged. To replace all text, explicitly select it first: `["meta", "a"]` on macOS or `["ctrl", "a"]` on Windows, then type.

On Windows, `computer_type` sends each LF, CRLF, or standalone CR as one Enter keystroke. Multiline editors normally insert a line break; other controls may submit a form or perform their usual Enter action.

All windows and the desktop share one actionable snapshot. It expires 60 seconds after `created_at`, at the returned `expires_at` time. A new observation replaces it, and a wait or window mutation invalidates it. An input attempt consumes it once it reaches native dispatch, including an uncertain native failure. A request rejected during argument or input-precondition validation can leave it usable. After `stale_snapshot`, observe the intended target again and use the new references and image coordinates.

Point input requires the current snapshot's image. Set `screenshot:true` in the same observation when you need both controls and image coordinates. After a tree-only observation, you can click an `element_id` from the new snapshot or request a fresh image for point input. The origin is the top-left corner of that image, including after cropping and resizing. Invalid point errors distinguish a missing screenshot from a point outside `image_width` and `image_height`. For a window crop outside the target, the error gives the window's bounds in desktop coordinates.

Pass `observe_after` as an empty object on `computer_click` or `computer_drag` to return the next window observation under `observation`. Add `"screenshot":true` inside `observe_after` for JPEG content and use the returned `observation.snapshot_id` and image for the next action. A new screenshot of the same window reuses the capture mode and width limit. It reuses the previous crop relative to the window when it still fits. Desktop follow-up requires `"screenshot":true` and reuses the desktop region and width limit.

On Windows, the follow-up can observe a foreground window directly owned by the input target, or return to the target's owner after its dialog closes. The server briefly waits for the application to process the input. For slower transitions, use `computer_wait` or observe again. If `input_delivered:true` is paired with `observation_error`, the input was sent. Obtain a fresh observation before deciding whether to repeat it.

Add `focused:true` to a window `observe_after` to return `observation.focused` with the active window's focused control role, available name, and desktop bounds. A screenshot-only follow-up can return this summary with the image without traversing the accessibility tree. An unavailable focused control is omitted. A failed lookup can return `observation.focus_error` alongside the observation.

When Windows can verify a visible menu's owner, the follow-up identifies it with `window.menu:true` and `mouse_active:true`. With `observe_after:{"screenshot":true}`, it returns a screen capture of the menu rectangle and uses `capture_mode:"screen"`. Use that image and snapshot to click the menu. With `observe_after:{}`, it requests the menu's accessibility elements. To refresh the latest menu observation while the menu is open, pass its `window_id` to `computer_observe`. If a menu cannot be verified, `observation_error.suggested_region` contains a desktop crop when the bounds are available. Pass it as `region` with `desktop:true` to inspect it separately.

On Windows, use `element_id` for an observed popup control, including one outside the main window. The server verifies the popup's ownership and that the hit matches the requested control before sending a mouse click. After `stale_snapshot` during a popup transition, observe the window again and use the new references. For a visible popup without a verified element, observe a small desktop region around it and use that image's coordinates.

On Windows, coordinate clicks return `hit` after confirmed delivery, with the pre-dispatch `desktop_point` and the hit's top-level `window_id` when available. For either coordinate or element clicks, use `report_hit:true` to also query the UI Automation `control` with `role`, optional `name`, and desktop `bounds`. `control_error` explains an unavailable control. Set `report_hit:false` to omit `hit`. Use a subsequent observation to check how the application responded.

On macOS, observe an open menu with a window screenshot using `capture_mode:"screen"`. Coordinate clicks and mouse moves can target a menu inside the image when accessibility confirms it belongs to that window. If the menu extends outside the window or its owner cannot be verified, observe a small desktop region around the visible menu.

For the Dock, taskbar, menus outside a verified window, or an empty desktop, use `computer_observe({"desktop":true})`. This returns a screenshot of the first display without a window or semantic elements. Set `region` to a desktop rectangle from `computer_status` to select another display or capture across displays. Desktop coordinate input checks the display layout and can operate without a focused window.

`computer_wait` accepts an exact `window_id` and combines optional `text`, `active:true`, `focused:true`, and exact `focus_name` conditions. Either focus condition requires an active window. Text matches case-sensitive substrings of accessibility names and non-password values. When the conditions do not match before `timeout_ms` (default 5,000, maximum 10,000), the tool returns `matched:false`. Request cancellation and native errors remain errors.

Activation waits for confirmed window focus or a non-key panel's mouse-ready state. Activation and keyboard preparation default to 2,000 ms and accept `timeout_ms` up to 10,000. A confirmation deadline reports `activation_failed`, and an identified modal blocker reports `window_blocked`. Keyboard tools accept `auto_activate:true` and `focus_name` within a shared preparation budget.

Windows can deny foreground activation. This returns `activation_failed` with the target and current foreground window IDs when available. Activate the target manually, then observe it again. Activation never sends hidden keystrokes or clicks to bypass the restriction.

`window_blocked` includes the blocking window's `window_id` in `detail` when available. On macOS, attached sheets are children of their parent window and some modal blockers return a description. When the blocker has no separate ID, observe a small desktop region around the visible sheet or dialog.

`computer_click`, `computer_scroll`, and `computer_drag` accept `hold_keys`, an optional array of unique `ctrl`, `alt`, `shift`, `meta`, and `space` keys. These combined actions require a window snapshot whose window has `active:true`. The keys are released after the action, including on cancellation or failure. For example, `"hold_keys":["space"]` may pan a canvas during a drag if the application supports it.

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

Use `computer_batch` after confirming the brush and canvas in a window screenshot. Pass its `snapshot_id` and 1–8 `steps` such as `{"op":"drag","path":[{"x":10,"y":20},{"x":40,"y":60}]}`. Every path uses that image's coordinates. Each step is one left-button stroke with 2–128 points and an optional `duration_ms` (100–3,000, default 500). The batch permits up to 256 path points and 6,000 ms of planned drag time in total.

The batch validates all arguments before sending input, consumes the snapshot at the first native dispatch, and checks the window and each stroke's vertices before sending it. All steps run in order under one desktop lock. The first failure stops the batch without retry. The ordered `steps` result reports `status` (`completed`, `failed`, or `not_run`) and `input_delivery` (`sent`, `not_sent`, or `unknown`). A stopped batch returns an MCP tool error with the zero-based `stopped_at` index, error details, and its step results. Previously sent strokes remain in the application. Native failure during a stroke can leave some events delivered (`unknown`). Observe again before changing brushes or deciding how to continue after a failure.

Pass `observe_after:{"screenshot":true}` to return one JPEG and a new `observation.snapshot_id` for the same window after the whole batch succeeds. The follow-up reuses the capture mode, width limit, screenshot-only setting, and crop when it still fits. If the drawing window loses readiness or capture fails, the result retains the completed steps and reports `observation_error`.

If the server can return a response after timeout or cancellation, it reports the last confirmed progress. A stroke in native dispatch can be `unknown`. Client cancellation or disconnect can prevent delivery of the entire response. Observe the canvas before resuming whenever the result is unavailable.

Observation supports case-sensitive `text` substrings, case-insensitive exact native `role` matches, `region`, `compact:true`, and `max_depth` (1–64, default 64). `compact` omits unnamed, empty, unfocused structural containers while retaining their descendants. `region` filters intersecting semantic elements even without a screenshot. `root_id` with the current `snapshot_id` scopes traversal to a subtree.

Set `screenshot_only:true` on a window observation to return a JPEG and an actionable window snapshot without traversing accessibility elements. It implies `screenshot:true` and supports image settings such as `region`, `max_width`, and `capture_mode`. Semantic filters and diffs require an observation with accessibility traversal. An `observe_after` screenshot of the same window preserves screenshot-only mode and uses the same geometry and hit checks for coordinate input.

Pass `since_snapshot_id` from the latest successful observation for a diff; that observation must be in window mode with the same window, filters, element budget, and OCR setting. It remains a diff base after input consumes its actionable references. `elements` then contains new/changed nodes; remove `removed_element_ids` and use `element_order` to reconstruct the current bounded result. `diff_complete:false` means at least one observation was truncated: removal from the result does not establish removal from the application. Use the new `snapshot_id` for every subsequent action, including unchanged elements. Native identity changes are represented as removal/addition. Diffs reduce response size, not native traversal work.

On macOS, `ocr:true` runs local Vision recognition on the returned screenshot with English and Simplified Chinese enabled (`screenshot:true` is required for window observations). It returns `ocr` text regions bounded by `max_elements`, with confidence and image coordinates, plus `ocr_truncated` when needed. In window capture mode, unnamed controls with no AX value may receive `ocr_text`; native names and click validation remain separate. Screen captures return independent OCR regions because visible text may belong to an overlapping window. Text overlapping password controls present in the semantic result is omitted from OCR output. Other visible sensitive text can still appear, particularly in screenshot-only or filtered observations. OCR is opt-in, adds recognition latency, and does not upload images. `computer_status.ocr` reports support; this npm Windows distribution returns `unsupported_operation` for OCR.

## Limits and behavior

- Observation defaults to 200 elements; maximum 1,000. Tree traversal scans at most 5,000 nodes, has a three-second budget, and reports `truncated` when incomplete. Text values and names are bounded. Provider-specific delays can exceed the traversal budget.
- Windows adds `truncation_reasons` when traversal is incomplete. `element_limit` is the output budget, `node_limit` is the scan budget, `time_limit` is the traversal deadline, `depth_limit` is `max_depth`, and `provider_error` indicates UI Automation could not read part of the tree. Set `COMPUTER_MCP_UIA_DIAGNOSTICS=1` to log the first provider failure of each traversal, including the operation and available HRESULT, to stderr.
- Screenshots default to a maximum width of 1,440, configurable from 320 to 2,560; height is capped at 2,560. Window observations default to `capture_mode:"window"` (ScreenCaptureKit on macOS, PrintWindow on Windows). Use `capture_mode:"screen"` for the visible desktop region including overlaps; desktop mode always uses screen capture. Windows applications must support PrintWindow rendering; there is no automatic screen fallback. Screenshots return as MCP image content, not files. Window source buffers and Windows screen regions are capped at 40 million pixels.
- Window mouse input rechecks geometry and readiness. Element clicks also revalidate the element. Hit testing covers the click, move or scroll point and each supplied drag vertex. Checks can race external changes, and the segments between drag vertices are not checked pixel by pixel. A failed hit test returns `target_obscured`. On Windows, `detail` includes the reason, desktop point, and available target, hit and owner window IDs. On macOS, it can identify the hit role and window ID or why ownership could not be verified.
- Requests have a 15-second budget including queue time. A single worker executes desktop requests under a per-user file lock. `busy` means the local queue of 16 is full or another instance holds the lock. A timed-out native call still occupies the worker and lock until it returns.
- Clicks are physical mouse events. Keyboard chords use virtual keys; macOS letter/digit shortcuts use physical US-layout key positions. Unicode text insertion is independent of these shortcuts. Custom-rendered controls may require image points and a vision-capable agent.
- Tool success means the input was delivered or the OS call was accepted. It does not prove the application completed the intended task. Timeouts and cancellation never trigger automatic input retries.
- Request-level timeouts and cancellations reported by the server include `timing.stage` and `timing.elapsed_ms` for the current stage. After confirmed input delivery, timing appears under `observation_error` if follow-up observation times out or is canceled. `computer_status`, `computer_observe`, and `computer_wait` report that no input was sent. Set `COMPUTER_MCP_TIMINGS=1` to log stage durations and tool names on stderr. On macOS, capture distinguishes `capture_shareable_content`, `capture_image`, and `capture_encode`. If ScreenCaptureKit stalls while several instances use the same executable path, stop the other instances and restart the affected one.
- Password values are omitted from semantic snapshots. Screenshots can still include sensitive visible content. Desktop text is untrusted input for the agent.

## Drawing a postcard in Freeform

Codex used GPT-6.2-Sol (high) and Computer MCP to draw this postcard for a stranger in Freeform on macOS.

![Illustrated postcard with a lighthouse and an orange moon over the sea, captioned “May you have light along the way”](images/freeform-postcard.png)

Prompt translated from Chinese

> Open Freeform on macOS and draw a postcard you would like to give to a stranger. You decide what to draw and how to draw it.
>
> Please do not resize the Freeform window.
