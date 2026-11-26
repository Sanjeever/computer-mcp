# Development and release

[简体中文](development.zh-CN.md)

This guide covers local builds, verification, architecture decisions, and release operations. Supported systems and desktop permissions are defined in the [README](../README.md).

Use the Go version in [go.mod](../go.mod) and the pnpm version in [package.json](../package.json). Use the latest patch of the Node.js major selected by [CI](../.github/workflows/ci.yml) for development. The launcher's `engines` field describes the published package. Build on the target OS and architecture. macOS requires Xcode Command Line Tools. Windows uses the Go toolchain without cgo.

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm build:native
pnpm smoke
```

The launcher build writes `dist/cli.js`. The native build writes `build/computer-mcp` on macOS or `build/computer-mcp.exe` on Windows. The [pnpm workspace configuration](../pnpm-workspace.yaml) excludes optional platform packages from source installs so development works before those packages are published.

To check installed packages locally after building:

```sh
pnpm pack:platform
npm pack --pack-destination artifacts --ignore-scripts
pnpm pack:check
```

Use freshly built launcher and host platform tarballs in `artifacts/`. `pack:check` installs them offline in a temporary directory and runs the same stdio smoke check through the installed launcher. `smoke` negotiates MCP, checks the tool count and read-only status, then closes stdin and waits for the process within the smoke deadline. It sends no desktop input.

## Architecture decisions

The backend uses one locked OS thread to preserve UIA COM lifetimes. Provider calls can outlive the request deadline, so the worker retains the per-user lock until the native call returns to prevent an overlapping operation. See the [request limits](usage.md#limits-and-behavior).

`computer_batch` uses one job and one desktop lock. It plans image-coordinate transforms before the first stroke, consumes the public snapshot at dispatch, and keeps the transform local to the job. Each stroke still has its own target and path checks and mouse button lifetime. The checks can identify changes to window geometry and occlusion. Changes to the brush or canvas inside the window are application state that the caller should confirm between batches.

The batch publishes completed strokes and the current dispatch state before it enters a potentially blocking provider. A timeout response can include those receipts while the worker still holds the desktop lock. Publishing the next stroke as uncertain before the last cancellation check prevents a canceled caller from seeing `not_sent` while dispatch can still proceed.

Window and element IDs follow native identity to keep title edits and traversal order changes from retargeting input. macOS preserves window mappings across temporary enumeration gaps and retains AX identity when a screen-window match is ambiguous.

Window capture and screen capture are explicit choices because an unobscured window image and the visible input target can differ. OCR text from screen capture may belong to an overlapping window, so it stays separate from native controls. The Windows npm distribution has no MSIX package identity, so OCR is currently available through macOS Vision. See the [tool contract](usage.md#tools).

Windows element clicks use UIA bounds in physical desktop pixels. Windows 10 can report a successful UIA cache refresh after an HWND is destroyed, so resolving an element also checks its cached native handle when present. For a hit in another root window, the native backend verifies its same-process owner chain and checks that it contains the observed element's bounds. UIA hit testing matches the requested element in the control view and can follow an icon or text child to its control. The owned-popup fixture exercises a control beyond the main window, an unowned covering window, a covering control inside the popup, and a reference to the destroyed popup control.

Windows follow-up checks the foreground GUI thread's menu state for native menus and the owner chain for titleless foreground popups. A screen capture stays within the verified menu bounds. The server checks menu identity and geometry again after observation and before using the menu snapshot for input.

## Verification

[CI](../.github/workflows/ci.yml) packs the launcher once on Linux to preserve the executable mode of its entry point. Both native jobs install and test that tarball, then upload their respective platform packages. Use the CI macOS deployment environment when running Go tests or vet directly. [`build:native`](../scripts/native.ts) sets the deployment target for native builds.

The [server tests](../internal/server/) use a fake backend for desktop-dependent contract checks. The [catalog tests](../internal/i18n/i18n_test.go) check key parity, and the [launcher tests](../src/launcher.test.ts) cover package selection and missing dependencies. Native interaction coverage is maintained with the [macOS desktop](../internal/desktop/integration_darwin_test.go), [macOS server](../internal/server/coordinates_darwin_test.go), [Windows desktop](../internal/desktop/integration_windows_test.go), and [Windows capture](../internal/desktop/capture_windows_test.go) fixtures.

Native UI acceptance should use a dedicated test window in an interactive session. Verify Unicode text, click, keyboard selection, scrolling, dragging, screenshots, focus rejection, and stale references. Repeat at multiple scaling factors and with secondary monitors before claiming those configurations are validated. Do not automate input into personal applications during tests.

On macOS, use the CI deployment environment and run the fixture packages serially.

```sh
COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server
```

On Windows, run the desktop fixture package in an interactive session.

```powershell
$env:COMPUTER_MCP_NATIVE_TEST = "1"
go test -p 1 -v ./internal/desktop
Remove-Item Env:COMPUTER_MCP_NATIVE_TEST
```

Keep the desktop idle so the fixture windows retain focus. Windows fixture setup can click its own verified edit control to acquire foreground access. macOS fixtures report missing Accessibility or Screen Recording permissions as skips. Hosted CI leaves interactive fixtures disabled and does not verify permission attribution or signed upgrades.

The Windows capture fixtures check updates to the pixels of an edit control and compare window and screen capture of a dedicated WPF window. The WPF fixture uses Windows PowerShell and logs its rendering tier and DPI with the test result.

The macOS `stdio drawing batch` case sends the same eight strokes and 256 points through sequential `computer_drag` calls with `observe_after` and through one `computer_batch` call. It verifies order and stroke boundaries from fixture events and logs calls, JPEGs, and elapsed time after the initial observation. `TestNativeWindowsDrawingBatch` uses cropped and scaled window and screen images through the Windows stdio server. It checks independent press, drag, and release sequences, the final desktop point, and the follow-up JPEG.

On a Windows test machine without the Go toolchain, copy the desktop test executable from `go test -c ./internal/desktop` and a native server built from the same checkout. Set `COMPUTER_MCP_TEST_BINARY` to the server's absolute path before running the fixture executable. The follow-up observation fixtures exercise an owned dialog, a Win32 context menu, and a titleless owned popup through the stdio server.

## Release

The root [package.json](../package.json) version is authoritative. Native builds embed it. Update it and the two `optionalDependencies` versions together, then run `pnpm install --lockfile-only`. Add `docs/releases/X.Y.Z.md` and `docs/releases/X.Y.Z.zh-CN.md` with user-facing changes, upgrade notes, and the verification actually performed. Commit the version, notes, and any lockfile changes together. The English file supplies the GitHub Release body.

Run `node --import=tsx scripts/release.ts check vX.Y.Z` from the repository root to check release inputs. Run [CI](../.github/workflows/ci.yml) on the release commit for a build without publication. Push `vX.Y.Z` once the commit has entered `main` and is ready to publish. The [Release workflow](../.github/workflows/release.yml) checks the tag, version, platform dependencies, notes, and `main` ancestry before calling CI.

### First release

1. Create the GitHub `npm` environment and allow deployments from `v*` tags. A required reviewer can hold the publish job until the tagged artifacts have been checked. If the repository maintainer will approve their own deployment, keep self-review enabled. See [deployment protection rules](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments).
2. Establish ownership of the launcher and both platform packages. For package names absent from npm, sign in as the scope owner and use `npm stage publish <tarball> --access public --tag bootstrap` on each previously checked bootstrap tarball. Use a version distinct from the stable release, since a pending staged version reserves its version number. npm creates a public `0.0.0-stage` placeholder and keeps the bootstrap tarball pending. See [staged publishing](https://docs.npmjs.com/staged-publishing/).
3. In the settings for **each** package, add an npm GitHub Actions Trusted Publisher with owner `Sanjeever`, repository `computer-mcp`, workflow `release.yml`, environment `npm`, and **Allow npm publish**. Configure these within two days of the first successful OIDC publish. See [npm Trusted Publishers](https://docs.npmjs.com/trusted-publishers/).
4. Verify the final packages on interactive macOS and Windows desktops, including the actual client launch path and permission prompts outside the server. If deployment approval is enabled, download `release-assets` from the tag run while the publish job is waiting. Check the manifest with `node --import=tsx scripts/release.ts verify vX.Y.Z <directory>` from the tagged checkout before using the tarballs.

If a placeholder cannot be configured for OIDC, use the same `release-assets` tarballs from the tagged run for a one-time authenticated publication. Stage and approve the two native packages first, then the launcher, using the `latest` tag for this stable version. The publish job verifies the matching npm integrity values and completes the GitHub Release. A manually published version will lack the workflow's automatic npm provenance. Reject pending bootstrap versions once the stable release is live. Configure or refresh Trusted Publishers before the next OIDC release.

### Publication and recovery

The publish job downloads `release-assets`, verifies the included manifest, and creates or resumes an unpublished GitHub Release with the three tarballs. It publishes both native packages before the launcher via npm OIDC with provenance, then makes the Release public. Verify a clean registry install on each supported platform with `npx -y @sanjeever/computer-mcp@X.Y.Z --version`, then connect through the client and run `computer_status`.

If publication fails, use **Re-run failed jobs** on the original run to reuse its `release-assets`. The script skips an npm version if its integrity equals the original tarball and uploads only missing draft assets. Query failures, incomplete or mismatched assets, and conflicting npm versions stop publication. Inspect any incomplete draft upload before removing it. If the original artifact has expired or npm reports conflicting bytes, reconcile the existing publishes and prepare a new version if necessary. npm versions are immutable.

The [platform packaging script](../scripts/platform.ts) includes third-party license notices with the precompiled binaries. The release workflow currently omits Apple Developer ID signing and notarization and Windows Authenticode signing. Verify permission persistence across signed upgrades before documenting it as supported.
