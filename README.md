# Wave

Wave captures a structured inventory of a macOS machine and provides CLI, TUI,
and local web interfaces for reviewing migration plans.

> [!WARNING]
> Apply supports vetted root dotfiles, captured applications, and captured
> preferences. Rollback restores files and settings but retains installed apps
> for manual cleanup because Wave cannot prove exclusive ownership. Manual apps
> without install payloads, `.config`, nested files, and credentials are not applied.

## Current Capabilities

| Workflow | Status |
| --- | --- |
| Capture device inventory to YAML or JSON | Available |
| Preview a state file with CLI | Available with `--dry-run` |
| Navigate capture and preview workflows in TUI | Available |
| Capture and preview through a local web GUI | Available |
| Restore vetted root dotfiles from a `.wave` archive | Available with confirmation |
| Conflict-safe rollback | Available for applied root dotfiles |
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

Download version `1.1.0` or newer from the
[releases page](https://github.com/GOI17/wave/releases). Do not use the older
`v1.0.0` binary for migration workflows.

```bash
# Apple Silicon
curl -LO https://github.com/GOI17/wave/releases/download/v1.1.0/wave-Darwin-arm64
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
wave capture --output wave-state.wave
```

Keep `.wave` archives private. They contain vetted root dotfile contents plus
application and preference metadata. Known credential paths/content, `.config`,
nested files, and symlinks are excluded.

Preview the migration plan without changing the machine:

```bash
wave apply --input wave-state.wave --dry-run

# Apply only after reviewing the preview
wave apply --input wave-state.wave --confirm

# Restore the latest transaction; post-apply edits become conflicts
wave rollback --confirm
```

## TUI

```bash
wave tui
```

Use arrow keys or `j`/`k` to navigate, Enter to select, and `q` to quit.
The migration option is explicitly preview-only and runs CLI apply with
`--dry-run`.

The default state path is `~/wave-state.wave`:

- **Capture Device State** writes that file.
- **Preview Migration (Dry Run)** validates and previews it.
- **Apply Migration** requires `y` confirmation.
- **Rollback Latest Migration** requires `y` confirmation.

## GUI

```bash
wave gui
# Optional custom port
wave gui --port 8081
```

Wave binds only to `127.0.0.1`, opens the default browser, and prints the local
URL. Press Ctrl+C in the terminal to stop the server. If the browser does not
open, visit the printed URL manually.

The GUI supports capture, preview, confirmed Apply, and confirmed Rollback.
Type `APPLY` or `ROLLBACK` before mutation.

## Commands

```text
wave capture [--output setup.wave]
wave apply --input setup.wave --dry-run
wave apply --input setup.wave --confirm
wave rollback [--transaction ID] --confirm
wave update
wave uninstall --confirm
wave tui
wave gui [--port 8080]
wave verify --input FILE  # placeholder
wave diff                 # placeholder
wave version
wave --help
```

## Safety Limits

- Immediate root dotfiles that pass strict safety checks are bundled and applied.
  Homebrew packages, VS Code extensions, App Store apps available through `mas`,
  and captured Finder, Dock, keyboard, computer name, timezone, and language
  settings are also applied. Manual apps are reported for manual installation.
- `.config`, nested files, symlinks, and credentials are never mutated.
- Rollback restores exact original file identity. If a file changed after
  Apply, Wave preserves it and reports a conflict instead of overwriting it.
- Applications installed during Apply are intentionally retained during
  Rollback and listed for manual cleanup; Wave never uninstalls software it
  cannot prove is exclusively owned by the transaction.
- Transaction data is retained under `~/.wave/transactions` for recovery.
- Malformed transaction metadata blocks Apply until explicitly quarantined with
  `wave transactions quarantine --transaction ID --confirm`.
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
