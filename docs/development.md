# Development and release

[简体中文](development.zh-CN.md)

This guide covers local builds, verification, architecture decisions, and release operations. Supported systems and desktop permissions are defined in the [README](../README.md).

Use the Go version in [go.mod](../go.mod), and the Node.js and pnpm versions declared by `engines` and `packageManager` in [package.json](../package.json). macOS builds require Xcode Command Line Tools; Windows builds use Go's Windows toolchain without cgo. Build native binaries on the corresponding OS and architecture.

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm build:native
pnpm smoke
```

The complete check sequence, including typecheck, lint, formatting, Vitest, Go tests/vet, and package checks, is maintained in [CI](../.github/workflows/ci.yml). On macOS, set these before running Go test/vet commands directly; `build:native` sets them automatically:

```sh
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_CFLAGS="-mmacosx-version-min=14.0"
export CGO_LDFLAGS="-mmacosx-version-min=14.0"
```

The Go toolchain may download the version required by `go.mod` when the local version is older. The [pnpm workspace configuration](../pnpm-workspace.yaml) excludes this project's optional platform packages from source dependency installation so frozen installs work before those packages are published. Local builds and smoke tests use `build/` directly; package checks install the newly built tarballs.

To check installed packages locally after building:

```sh
pnpm pack:platform
npm pack --pack-destination artifacts --ignore-scripts
pnpm pack:check
```

`pack:check` needs both tarballs. It installs them offline into an isolated temporary directory and runs the launcher through the stdio smoke check. `smoke` negotiates MCP, lists tools, calls the read-only status tool, and verifies EOF shutdown; it sends no desktop input.

## Architecture decisions

The backend uses one locked OS thread to preserve UIA COM lifetimes and serialize desktop operations. UIA and AX use bounded traversal and provider timeouts, but a native call can outlive a request deadline. The worker and cross-process lock remain occupied until that call returns so another operation cannot overlap it. The request and queue limits are defined in the [README](../README.md#limits-and-behavior).

Native references stay inside `internal/desktop`. Window and element IDs follow native identity so title edits and traversal order changes do not retarget input. On macOS, window mappings survive temporary enumeration gaps; ambiguous matches retain AX identity instead of guessing by title.

Window capture and screen capture are explicit choices because an unobscured window image and the visible input target can differ. Hit testing still rejects covered window targets. OCR from screen capture stays separate from native controls because the text may belong to an overlapping window. See the [tool contract](../README.md#tools) for capture modes, coordinates, focus, and snapshot recovery.

## CI

[CI](../.github/workflows/ci.yml) runs checks and package smoke tests on both supported platforms, then uploads the verified tarballs. Hosted runners do not verify macOS permissions, signed upgrades, or arbitrary application interaction.

- [Server tests](../internal/server/) cover snapshot invalidation, coordinate mapping, focus and input validation, cancellation, password redaction, OCR, diffs, and MCP contracts using a fake backend where desktop interaction is involved. [Catalog tests](../internal/i18n/i18n_test.go) check language-key parity; [launcher tests](../src/launcher.test.ts) check platform selection and missing-package errors.
- [Desktop fixture tests](../internal/desktop/integration_darwin_test.go) cover native macOS input, window identity, text extraction, capture, and OCR. [Server fixture tests](../internal/server/coordinates_darwin_test.go) verify clicks against actual JPEG targets, follow-up images after resize/crop and window movement, floating panels, held-key clicks, continuous drags, cancellation cleanup, and real stdio capture.
- [Windows fixture tests](../internal/desktop/integration_windows_test.go) cover Unicode and multiline input in dedicated Edit and RichEdit controls, UIA values and focus, native activation, mouse movement, held-key clicks/scrolls/drags, and release after drag cancellation. The stdio fixture test opens and closes an owned dialog using the snapshot returned by each click. [Capture tests](../internal/desktop/capture_windows_test.go) check JPEG dimensions without a desktop and cropped/scaled PrintWindow and screen captures in the interactive fixture. RichEdit UIA values use CR paragraph separators and include a final paragraph mark; they need not match an application's saved-file line endings.

Native UI acceptance should use a dedicated test window in an interactive session. Verify Unicode text, click, keyboard selection, scrolling, dragging, screenshots, focus rejection, and stale references. Repeat at multiple scaling factors and with secondary monitors before claiming those configurations are validated. Do not automate input into personal applications during tests.

On macOS, set the cgo flags above, then run:

```sh
COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server
```

On Windows, run in an interactive desktop session:

```powershell
$env:COMPUTER_MCP_NATIVE_TEST = "1"
go test -p 1 -v ./internal/desktop
Remove-Item Env:COMPUTER_MCP_NATIVE_TEST
```

Run native packages serially so their fixture windows do not compete for focus, and leave the desktop idle. Windows fixtures may explicitly click their own verified edit control to acquire foreground access during setup; production activation does not synthesize input. Tests skip with a reason when macOS Accessibility or Screen Recording permission is absent. CI leaves interactive fixtures disabled; Windows status and stdio checks do not establish compatibility with arbitrary applications, fonts, scaling factors, or displays.

## Release

The root [package.json](../package.json) version is authoritative; native builds embed it and platform packaging checks it. Update it and both `optionalDependencies` versions together, then run `pnpm install --lockfile-only`. Commit the lockfile and relevant bilingual documentation.

[Release](../.github/workflows/release.yml) supports two entry points:

- Push a `vX.Y.Z` tag to build, verify, publish the three npm packages, and create a GitHub Release.
- Run the Release workflow manually with `publish=false` to build and upload artifacts without publishing. With `publish=true`, select a matching version tag.

The tag must match `package.json`. The workflow publishes native packages before the launcher and uses the CI tarballs without rebuilding them. npm versions are immutable: inspect partial publication before rerunning a failed publishing job.

Before the first public release, establish ownership of the launcher package (`name` in [package.json](../package.json)) and both packages listed in its `optionalDependencies`. Configure npm trusted publishers for the repository named there, workflow `release.yml`, and GitHub environment `npm`.

Create the GitHub `npm` environment and set its protection rules. Initial package creation and npm account setup may require an authenticated bootstrap publication; that is an account operation performed separately from this workflow. Subsequent releases use OIDC with `id-token: write` and npm provenance. No `NPM_TOKEN` is embedded or required by the workflow.

The workflow ships native binaries linked to system frameworks and includes third-party license notices. It currently does not perform Apple Developer ID signing/notarization or Windows Authenticode signing. Introducing trusted signing requires the corresponding account, certificate, and CI secret configuration; verify permissions across upgrades before advertising persistent authorization.
