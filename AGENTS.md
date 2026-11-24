# Computer MCP

[简体中文](AGENTS.zh-CN.md)

Read [README.md](README.md) for the user contract and [docs/development.md](docs/development.md) for build, verification, and release procedures. English is the primary documentation language. Use English Conventional Commits.

## Architecture

- Go owns the stdio MCP server, tool contracts, desktop state, bounded execution, and native adapters. Use the official MCP Go SDK.
- TypeScript owns the npm launcher and packaging scripts. The compiled launcher inherits stdio and starts the native binary directly. Use the toolchain and target build environments in the [development guide](docs/development.md).
- Publish only the OS and architecture combinations in the [README](README.md).
- Native access is confined to `internal/desktop`. macOS uses a thin Objective-C bridge; Windows uses generated Win32/UIA bindings. Keep all backend calls and COM object lifetimes on the engine's locked OS thread.
- Use the existing backend boundary. Do not introduce plugin registries, services, network transports, or additional languages without a concrete requirement.

## Behavioral contracts

- stdout is exclusively the MCP transport; diagnostic output belongs on stderr. Never log typed text, screenshots, or whole desktop snapshots by default.
- Use snake_case for tool names, fields, and error codes. Keep these and opaque IDs language independent. Keep tool descriptions and application errors in `internal/i18n`.
- Observations are bounded. Report truncation. Element references belong to the latest snapshot and expire; input consumes the actionable snapshot. Window mouse input validates geometry and active/mouse-ready state; element clicks also revalidate native identity and properties. Desktop snapshots validate display layout and allow system UI interaction. Keyboard input requires an active window.
- Image coordinates refer to the returned image, including crop and resize. Convert centrally. Preserve negative desktop coordinates and mixed-DPI behavior.
- Input is serialized and guarded by a per-user cross-process lock. Never retry an uncertain input operation. Cancellation cannot undo delivered input. Always release synthetic held keys/buttons.
- Native providers can block beyond a Go context deadline. Keep the worker occupied until the native operation returns; do not start overlapping operations on timeout.
- Do not silently fall back between accessibility actions and mouse events. Do not add clipboard, shell, browser DOM, network access, telemetry, or permission prompts to the native backend.
- Permission checks must not trigger consent; backend initialization is lazy. Do not inspect unrelated personal applications during automated tests. Native interaction tests must use a dedicated fixture window.

## Documentation sync

- In the same change as a schema, default, output, error, or behavior update, check the affected README, development guide, `internal/i18n` descriptions, comments, and examples against code and tests. Keep both languages equivalent and catalog keys in parity. Preserve native diagnostic details.
- Keep the user contract in README, contributor rules here, and procedures and design rationale in the [development guide](docs/development.md). Link to those sources or to configuration for versions, package names, platform support, and CI commands.
- Keep the reasons behind design decisions in docs and comments. When implementation conflicts with an established requirement, record the code or test evidence and confirm significant discrepancies before changing the contract.

## Verification and release

- Follow the [verification guide](docs/development.md) and run checks relevant to the change. For prose-only changes, check facts, links, examples, and bilingual consistency; builds and runtime tests are unnecessary.
- Keep tests meaningful: stale references, coordinate transforms, input validation, cancellation, bilingual catalog parity, MCP contracts, and installed package execution.
- A successful cross-compile is not native runtime validation. Hosted CI does not establish interactive desktop compatibility or macOS TCC consent behavior. State what was actually tested.
- Follow the [release version procedure](docs/development.md#release); do not maintain independent native or platform-package versions.
- Distribute precompiled binaries with dependency license notices. Do not compile or download binaries in install hooks. No npm credentials in source.
- Local implementation, testing, packaging, and commits are authorized. Ask before pushing, publishing tags, GitHub Releases, npm packages, or changing account settings unless the user explicitly authorizes them in the active session.
