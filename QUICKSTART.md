# Wave Quick Start

Use Wave today to capture a macOS inventory and preview migration actions.
Cross-machine live restore is not implemented yet.

## 1. Install

Install through the Wave tap:

```bash
brew tap GOI17/wave
brew install GOI17/wave/wave
wave version
```

The qualified formula name distinguishes this project from the unrelated Wave
Terminal package. After installation, use the normal `wave` command.

Update or remove the Homebrew installation with:

```bash
wave update
wave uninstall --confirm
```

To build from source instead:

```bash
git clone https://github.com/GOI17/wave.git
cd wave
make build
sudo mv wave /usr/local/bin/wave
wave version
```

## 2. Capture

```bash
wave capture --output wave-state.wave
```

Keep `wave-state.wave` private. It contains vetted root dotfile contents and
device metadata; known credential content and nested configuration are excluded.

## 3. Preview

```bash
wave apply --input wave-state.wave --dry-run
```

Review ready/skipped items, then apply with explicit confirmation:

```bash
wave apply --input wave-state.wave --confirm
```

Rollback the latest transaction:

```bash
wave rollback --confirm
```

Files edited after Apply are preserved and reported as conflicts.
Applications installed by Apply remain installed after Rollback and are listed
for manual cleanup.

## 4. Choose An Interface

Terminal UI:

```bash
wave tui
```

The TUI uses the full terminal and offers capture, preview, confirmed Apply,
confirmed Rollback, archive view, and exit.

Local web GUI:

```bash
wave gui
```

The GUI opens at `http://localhost:8080` and supports capture, preview, Apply,
and Rollback. Type `APPLY` or `ROLLBACK` to confirm mutation.

## Important Limits

- Apply changes vetted root dotfiles, Homebrew packages, VS Code extensions,
  App Store apps available through `mas`, and captured preferences.
- Manual apps are reported as unresolved because their bundles are not archived.
- `.config`, nested files, symlinks, and credentials are preview-only.
- Do not publish state files without reviewing them.
- Do not transfer private keys or credentials through Wave.
- Do not run CLI live apply against valuable user data.
- Keep an independent backup before testing migration behavior.

For complete command and safety details, read [README.md](README.md).
