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
wave capture --output wave-state.yaml
```

Inspect `wave-state.yaml` before sharing it. It may contain usernames, absolute
paths, installed applications, preferences, aliases, and environment metadata.

## 3. Preview

```bash
wave apply --input wave-state.yaml --dry-run
```

`--dry-run` is required for safe use. The current artifact does not contain
dotfile contents, so it cannot restore those files to another machine.

## 4. Choose An Interface

Terminal UI:

```bash
wave tui
```

The TUI uses `~/wave-state.yaml` and offers capture, dry-run preview, file view,
and exit. Verification is still a placeholder.

Local web GUI:

```bash
wave gui
```

The GUI opens the default browser at `http://localhost:8080`. It binds to the
loopback interface and supports capture and preview only. Press Ctrl+C to stop
it.

## Important Limits

- Do not treat a Wave state file as a backup.
- Do not publish state files without reviewing them.
- Do not transfer private keys or credentials through Wave.
- Do not run CLI live apply against valuable user data.
- Keep an independent backup before testing migration behavior.

For complete command and safety details, read [README.md](README.md).
