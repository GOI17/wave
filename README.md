# Wave

Wave captures a structured inventory of a macOS machine and provides CLI, TUI,
and local web interfaces for reviewing migration plans.

> [!WARNING]
> Cross-machine live restore is not ready. Current state files contain paths and
> checksums for dotfiles, but not their contents. Use `--dry-run` to inspect a
> plan. Do not use CLI live apply with valuable data.

## Current Capabilities

| Workflow | Status |
| --- | --- |
| Capture device inventory to YAML or JSON | Available |
| Preview a state file with CLI | Available with `--dry-run` |
| Navigate capture and preview workflows in TUI | Available |
| Capture and preview through a local web GUI | Available |
| Restore dotfile contents on another Mac | Not available |
| Verification and state diff | Placeholders only |

Wave currently inventories applications, selected dotfile paths, macOS
preferences, and shell environment metadata. It does not replace a backup tool.

## Install

### Homebrew (Recommended)

```bash
brew tap GOI17/wave
brew install GOI17/wave/wave

# Verify
wave version
```

The qualified formula name avoids a collision with the unrelated Wave Terminal
package already available in Homebrew. The installed command is still `wave`.

Upgrade or uninstall with:

```bash
brew upgrade GOI17/wave/wave
brew uninstall GOI17/wave/wave
```

### Release Binary

Download version `1.0.3` or newer from the
[releases page](https://github.com/GOI17/wave/releases). Do not use the older
`v1.0.0` binary for migration workflows.

```bash
# Apple Silicon
curl -LO https://github.com/GOI17/wave/releases/download/v1.0.3/wave-Darwin-arm64
chmod +x wave-Darwin-arm64
sudo mv wave-Darwin-arm64 /usr/local/bin/wave

# Verify
wave version
```

Intel Macs use `wave-Darwin-amd64` instead.

### Build From Source

Requires Go 1.23 or newer:

```bash
git clone https://github.com/GOI17/wave.git
cd wave
make build
./wave version
```

## Safe Quick Start

Capture the current machine:

```bash
wave capture --output wave-state.yaml
```

Review the generated file before using it elsewhere. State files can reveal
local paths, usernames, application names, and environment configuration. Do
not publish them without inspection.

Preview the migration plan without changing the machine:

```bash
wave apply --input wave-state.yaml --dry-run
```

## TUI

```bash
wave tui
```

Use arrow keys or `j`/`k` to navigate, Enter to select, and `q` to quit.
The migration option is explicitly preview-only and runs CLI apply with
`--dry-run`.

The default state path is `~/wave-state.yaml`:

- **Capture Device State** writes that file.
- **Preview Migration (Dry Run)** validates and previews it.
- **View Captured State** opens it with `less`.
- **Verify Migration** is currently a placeholder.

## GUI

```bash
wave gui
# Optional custom port
wave gui --port 8081
```

Wave binds only to `127.0.0.1`, opens the default browser, and prints the local
URL. Press Ctrl+C in the terminal to stop the server. If the browser does not
open, visit the printed URL manually.

The GUI supports capture and dry-run preview only. Live apply is rejected.

## Commands

```text
wave capture [--output FILE] [--format yaml|json]
wave apply --input FILE --dry-run [--format yaml|json]
wave tui
wave gui [--port 8080]
wave verify --input FILE  # placeholder
wave diff                 # placeholder
wave version
wave --help
```

## Safety Limits

- A capture does not bundle dotfile contents, so it cannot reproduce those
  files on another machine.
- State paths are absolute and may not exist on the target machine.
- Captures may include metadata for sensitive paths. Private SSH keys should
  never be transferred through Wave.
- TUI and GUI migration workflows are dry-run-only.
- CLI live apply exists for development but is not recommended for user data.
- Keep an independent, tested backup before experimenting with migration tools.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

Run tests with an isolated `HOME` when exercising capture or migration behavior
so tests cannot inspect or modify a real user profile.

See [DEVELOPMENT.md](DEVELOPMENT.md) for contributor commands and
[ARCHITECTURE.md](ARCHITECTURE.md) for the current design.
