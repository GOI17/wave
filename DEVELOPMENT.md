# Wave Development Setup

## Prerequisites

- Go 1.23+
- macOS 10.14+
- `brew install` for Homebrew integration

## Project Structure

```
wave/
├── cmd/wave/              # CLI entry point
├── internal/
│   ├── analyzer/         # Device state capture
│   ├── executor/         # Apply changes
│   ├── migrator/         # Orchestration
│   └── models/           # Data structures
├── ui/
│   ├── cli/              # Cobra commands
│   ├── tui/              # Bubble Tea (TODO)
│   └── gui/              # Tauri app (TODO)
└── scripts/              # Build, deploy scripts
```

## Development

```bash
# Install dependencies
go mod download

# Build CLI
go build -o wave ./cmd/wave

# Run capture
./wave capture --output state.yaml

# Run dry-run apply
./wave apply --input state.yaml --dry-run

# View help
./wave --help
```

## Implementation Roadmap

### Phase 1: Core Infrastructure
- [x] Project scaffolding & structure
- [x] Core data models (MigrationState, etc)
- [x] Analyzer interface for macOS
- [x] Executor interface for macOS
- [x] Migrator orchestration
- [x] CLI with Cobra (capture, apply, tui, gui)
- [ ] Build & test setup

### Phase 2: Analyzer Implementation
- [ ] Homebrew packages detection
- [ ] App Store apps detection
- [ ] Dotfiles discovery & hashing
- [ ] System preferences (Finder, Dock, Keyboard, etc)
- [ ] Shell environment (aliases, functions, exports)
- [ ] VS Code extensions
- [ ] System info (hostname, OS, etc)

### Phase 3: Executor Implementation
- [ ] Homebrew installation
- [ ] App Store app installation
- [ ] Dotfile copy with conflict resolution
- [ ] Preferences application (via `defaults` command)
- [ ] Shell configuration merge
- [ ] VS Code extensions installation
- [ ] Pre/post migration hooks

### Phase 4: CLI Enhancements
- [ ] Progress bars & logging
- [ ] Selective migration (pick categories)
- [ ] Verification & diff reporting
- [ ] Migration history
- [ ] Rollback capability

### Phase 5: TUI Implementation
- [ ] Interactive selection of migration components
- [ ] Real-time progress monitoring
- [ ] Configuration editing in TUI
- [ ] Migration preview

### Phase 6: GUI Implementation
- [ ] Tauri app scaffolding
- [ ] React frontend
- [ ] Device comparison view
- [ ] Migration wizard
- [ ] Settings management

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Contributing

- Follow Go code conventions
- Keep packages focused and single-purpose
- Add tests for new functionality
- Update README and docs with changes

## License

TBD
