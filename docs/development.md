# Development and release

[简体中文](development.zh-CN.md)

Use the Go version in `go.mod`, Node.js 22+, and the pnpm version in `package.json`. macOS builds require Xcode Command Line Tools and `MACOSX_DEPLOYMENT_TARGET=14.0`; Windows builds use Go's Windows toolchain and do not require cgo. No compiler is required on end-user machines.

```sh
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm fmt:check
pnpm test
go test ./...
go vet ./...
pnpm build
pnpm build:native
pnpm smoke
pnpm pack:platform
```

On macOS, set these before Go test/vet commands; `build:native` sets them automatically:

```sh
export MACOSX_DEPLOYMENT_TARGET=14.0
export CGO_CFLAGS="-mmacosx-version-min=14.0"
export CGO_LDFLAGS="-mmacosx-version-min=14.0"
```

The Go toolchain may download the version required by `go.mod` when the local version is older. Before the first platform packages are published, pnpm may report missing optional platform dependencies; local builds and smoke tests use `build/` directly.

Go tests cover snapshot identity, stale target rejection, image coordinate mapping, input validation, password redaction, MCP schema rejection, and translation parity. TypeScript tests cover platform selection and missing-package errors. `smoke` starts the actual stdio process, negotiates MCP, lists tools, calls the read-only status tool, and verifies EOF shutdown. It does not send desktop input.

The backend is serialized on one locked OS thread. UIA uses COM MTA, cached property requests, provider timeouts, and bounded traversal. macOS uses AX queries with messaging timeouts and ScreenCaptureKit. Raw native references stay inside the backend. A snapshot carries coordinate metadata; observation and screenshots are sequential, so they are not an atomic view of the desktop.

`internal/i18n/en.json` and `zh-CN.json` must have matching keys. English and Chinese READMEs describe the same contract. Keep OS error details intact. Use English Conventional Commits.

## CI

`.github/workflows/ci.yml` checks Go and TypeScript, builds on macOS arm64 and Windows x64, runs stdio smoke checks, packs the native npm package, and installs both native and launcher tarballs into an isolated temporary directory for another smoke check. It uploads those exact tarballs. Hosted runners do not verify macOS permissions, signed upgrades, or arbitrary application interaction.

Native UI acceptance should use a dedicated test window in an interactive session. Verify Unicode text, click, keyboard selection, scrolling, dragging, screenshots, focus rejection, and stale references. Repeat at multiple scaling factors and with secondary monitors before claiming those configurations are validated. Do not automate input into personal applications during tests.

On macOS, run `COMPUTER_MCP_NATIVE_TEST=1 go test -v ./internal/desktop` after setting the cgo flags above. This opt-in test creates its own Cocoa window, checks focus, inserts English/Chinese/emoji text, captures a JPEG, clicks a button, and observes the resulting value. It skips with a reason when Accessibility or Screen Recording permission is absent. The native interaction fixture currently covers macOS; Windows CI runs the native status and stdio checks.

## Release

The root `package.json` version is authoritative. Update it and both `optionalDependencies` versions together, then run `pnpm install --lockfile-only`. Commit the lockfile and relevant bilingual documentation.

Release supports two entry points:

- Push a `vX.Y.Z` tag to build, verify, publish the three npm packages, and create a GitHub Release.
- Run the Release workflow manually with `publish=false` to build and upload artifacts without publishing. With `publish=true`, select a matching version tag.

The tag must match `package.json`. The workflow publishes native packages before the launcher and uses the CI tarballs without rebuilding them. npm versions are immutable: inspect partial publication before rerunning a failed publishing job.

Before the first public release, establish ownership of these packages and configure npm trusted publishers for `Sanjeever/computer-mcp`, workflow `release.yml`, GitHub environment `npm`:

- `@sanjeever/computer-mcp`
- `@sanjeever/computer-mcp-darwin-arm64`
- `@sanjeever/computer-mcp-win32-x64`

Create the GitHub `npm` environment and set its protection rules. Initial package creation and npm account setup may require an authenticated bootstrap publication; that is an account operation performed separately from this workflow. Subsequent releases use OIDC with `id-token: write` and npm provenance. No `NPM_TOKEN` is embedded or required by the workflow.

The workflow ships native binaries linked to system frameworks and includes third-party license notices. It currently does not perform Apple Developer ID signing/notarization or Windows Authenticode signing. Introducing trusted signing requires the corresponding account, certificate, and CI secret configuration; verify permissions across upgrades before advertising persistent authorization.
