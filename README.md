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
| Restore explicitly selected documents and `.app` bundles | Available through `wave replica` directory snapshots |
| Conflict-safe rollback | Available for applied root dotfiles |
| Verify captured files, Homebrew versions, and supported preferences | Available; reports differences and unverified items |
| State diff | Not implemented; returns an error |

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

## Selected documents and application bundles (macOS)

For an explicit sample of personal documents and self-contained applications,
use a directory snapshot. This mode is separate from the conservative `.wave`
archive and does not reinstall applications through package managers:

```sh
wave replica capture --output ~/sample-replica \
  --document 'Documents/example.pdf' \
  --application 'Example.app'
# Transfer the entire private sample-replica directory to the destination.
wave replica apply --input ~/sample-replica --dry-run
wave replica apply --input ~/sample-replica --confirm
wave replica verify --input ~/sample-replica
```

Repeat `--document` and `--application` for additional items. Documents are
relative to the current home; application names refer to `/Applications`.
Transport the directory with metadata intact. For example, on macOS use
`tar --format=pax --acls --xattrs --fflags -cpf sample.tar sample-replica` and extract with
`tar --same-owner --mac-metadata --acls --xattrs --fflags -xpf sample.tar` into a
fresh directory. The receiving account
must be able to preserve the captured owner and groups. A plain file upload or
an extraction that changes ownership will correctly fail integrity validation.
Use full PAX format: the default restricted PAX format can truncate directory
timestamps when no other extended metadata forces a PAX header.
Capture preserves native metadata and records SHA-256 hashes, modes, ownership,
timestamps, flags, ACLs, extended attributes, internal symlinks and hard links.
Capture fails if the selected source changes or the copy loses recorded metadata.
Apply verifies every payload before writing, rejects different existing items,
and publishes each copied item without replacement. Reapplying identical items
is a no-op. Preview and verification do not write destination data.

This is a selected-item migration, not a complete system backup. Application
support data, accounts, licensing, system extensions and external dependencies
are not included automatically. Source and target must support the recorded
ownership and metadata. External symlinks and special files are rejected.
Apply is atomic per selected item, not across the full selection; if a later
item fails, earlier verified items remain. The dotfile `rollback` command does
not roll back replica items. Use disposable VMs for acceptance testing.

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
wave share [--input setup.wave]
wave update
wave uninstall --confirm
wave tui
wave gui [--port 8080]
wave verify --input FILE  # Read-only; fails on differences or unverified items
wave diff                 # Not implemented; returns an error
wave version
wave --help
```

## Safety Limits

- `verify` checks the items represented in an archive. It does not certify a
  complete Mac clone: documents, application data, ACLs, and extended attributes
  are outside this archive format. Excluded files, manual apps, and unsupported
  checks are reported as unverified and cause a nonzero exit status.
- Homebrew capture includes installed dependencies and versions. Inventory
  failures abort capture instead of silently producing an empty package list.
  Apply still installs by name and does not pin the source versions; `verify`
  reports any resulting version differences.

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

For local migration testing, use a disposable container with no host mounts:

```bash
make test-sandbox
```

This runs portable analyzer, archive, and file executor tests. The full
transaction/rollback suite runs on disposable macOS CI with simulated application
managers. Real Homebrew, App Store, and macOS preferences require a macOS VM.
See [TESTING.md](TESTING.md) for the isolation boundaries and VM acceptance steps.

On a disposable macOS VM or hosted CI runner:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Changing `HOME` alone does not isolate Homebrew, `/Applications`, or macOS
preferences. Do not use it as a sandbox for a real Apply.

See [DEVELOPMENT.md](DEVELOPMENT.md) for contributor commands and
[ARCHITECTURE.md](ARCHITECTURE.md) for the current design.
