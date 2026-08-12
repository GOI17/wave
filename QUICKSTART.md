# 🌊 Wave – macOS Device Migrator

**A comprehensive tool to replicate your entire macOS device setup – apps, dotfiles, preferences, environment – with CLI, TUI, and GUI interfaces.**

## What is Wave?

Wave is a device migration solution for macOS that captures your complete device configuration and can replicate it on any other Mac. Unlike traditional backup tools, Wave creates a structured, version-controllable snapshot of:

- 📦 **Applications** (Homebrew formulas, casks, App Store apps)
- 📝 **Dotfiles** (shell configs, editor settings, config files)
- ⚙️ **System Preferences** (Finder, Dock, Keyboard, Trackpad, etc.)
- 🔧 **Environment** (shell aliases, functions, exports, PATH)
- 🧩 **Developer Tools** (VS Code extensions, Node versions, etc.)

**Use Cases:**
- New Mac setup (mirror old Mac to new one)
- Environment standardization (teams sharing configs)
- System snapshots (version control your setup)
- Quick recovery (restore from version control)

---

## 🎯 Project Status

### ✅ Completed (Phase 1)
- [x] Go project scaffolding
- [x] Core data models (MigrationState, DeviceInfo, etc.)
- [x] Analyzer interface (stub for macOS)
- [x] Executor interface (stub for macOS)
- [x] Migrator orchestration
- [x] CLI framework (Cobra)
- [x] Project documentation

**Estimated completion:** 15% (Full MVP ~40 hours away)

### 🚀 Next (Phase 2)
- [ ] Implement Analyzer methods (system info, apps, dotfiles, prefs, shell config)
- [ ] Add integration tests
- [ ] Create end-to-end example

### 🛣️ Roadmap
- Phase 3: Executor implementation (apply migrations)
- Phase 4: CLI polish & verification
- Phase 5: TUI with Bubble Tea
- Phase 6: GUI with Tauri + React

---

## 📦 What You Get Right Now

### Files & Structure
```
wave/
├── README.md                  # This file
├── STATUS.md                  # Current progress & next steps
├── ARCHITECTURE.md            # Technical deep dive
├── DEVELOPMENT.md             # Setup & testing
├── PHASE2_GUIDE.md           # Implementation guidance
├── Makefile                   # Development commands
├── .gitignore                 # Git rules
├── go.mod                     # Go dependencies
│
├── cmd/wave/main.go          # CLI entry point
├── internal/models/           # Data structures
├── internal/analyzer/         # Device capture (TODO)
├── internal/executor/         # Apply migrations (TODO)
├── internal/migrator/         # Orchestration
└── ui/cli/                    # Cobra commands
```

### CLI Commands (Ready to Use)
```bash
wave capture                    # Export device state to YAML/JSON
wave apply --input state.yaml   # Apply state (with --dry-run for preview)
wave apply --dry-run            # Preview without making changes
wave tui                         # Launch terminal UI (stub)
wave gui                         # Launch desktop GUI (stub)
wave --version                  # Show version
wave --help                      # Show help
```

### Dependencies Included
- **Cobra** – CLI framework
- **Bubble Tea** – TUI framework (for Phase 5)
- **YAML/JSON** – Serialization libraries
- **spf13/viper** – Config management

---

## 🚀 Quick Start

### 1. Install Go
```bash
# macOS
brew install go

# Or download from https://golang.org/dl
```

### 2. Build Wave
```bash
cd wave
make build

# Or manually:
go build -o wave ./cmd/wave
```

### 3. Try It
```bash
# See what would be captured
make help

# Build and test
make build
./wave --version

# Test capture (will fail with TODOs, but CLI works)
./wave capture --output test.yaml --format yaml
```

### 4. Next Steps
- See STATUS.md for detailed progress
- See PHASE2_GUIDE.md to start implementing Analyzer
- Use Makefile for common tasks (`make help`)

---

## 🏗️ Architecture Overview

```
┌────────────────────────────────────────────────┐
│ UI Layer (CLI, TUI, GUI)                       │
└────────────────────────────────────────────────┘
           │
┌────────────────────────────────────────────────┐
│ Migrator (Orchestration)                       │
├─────────────────────────────────────────────────
│ • Capture() - Collect device state
│ • Apply() - Execute migration
│ • Validate() - Check compatibility
└────────────────────────────────────────────────┘
           │
      ┌────┴────┐
      │          │
┌─────▼──────┐  ┌─────────────┐  ┌──────────────┐
│ Analyzer   │  │ Executor    │  │ Models       │
│ (Capture)  │  │ (Apply)     │  │ (Data)       │
└────────────┘  └─────────────┘  └──────────────┘
      │              │
      └──────┬───────┘
             │
    System Commands & APIs
    (defaults, brew, files, etc)
```

### Core Concepts

**MigrationState** - A complete snapshot of device configuration
```yaml
version: "1.0.0"
created_at: 2025-01-15T10:30:00Z
device_info:
  hostname: MacBook-Pro
  os_version: "15.1"
applications:
  homebrew: [...]
  app_store: [...]
dotfiles:
  files: [...]
preferences:
  finder: {...}
  dock: {...}
environment:
  aliases: {...}
```

**Analyzer** - Captures current device state
```go
type Analyzer interface {
    AnalyzeDevice() (*MigrationState, error)
    AnalyzeApplications() (*ApplicationGroup, error)
    AnalyzeDotfiles() (*DotfilesGroup, error)
    AnalyzePreferences() (*PreferencesGroup, error)
    AnalyzeEnvironment() (*EnvironmentGroup, error)
}
```

**Executor** - Applies migration to target device
```go
type Executor interface {
    ExecuteApplications(*ApplicationGroup, bool) ([]Task, error)
    ExecuteDotfiles(*DotfilesGroup, bool) ([]Task, error)
    ExecutePreferences(*PreferencesGroup, bool) ([]Task, error)
    ExecuteEnvironment(*EnvironmentGroup, bool) ([]Task, error)
    ValidateState(*MigrationState) error
}
```

---

## 📚 Documentation

- **README.md** – Project overview & quick start
- **STATUS.md** – Current progress, next steps, metrics
- **ARCHITECTURE.md** – Technical design, data flow, components
- **DEVELOPMENT.md** – Setup, building, testing guidelines
- **PHASE2_GUIDE.md** – Detailed implementation guide for Analyzer phase
- **This file** – Quick reference guide

---

## 🛠️ Common Commands

```bash
# Development
make build           # Compile CLI
make run ARGS='...'  # Run with args
make test            # Run tests
make clean           # Remove artifacts
make fmt             # Format code
make tidy            # Update dependencies

# Examples
make capture         # Capture device state
make apply-dry       # Preview migration

# Help
make help            # Show all targets
./wave --help        # Show CLI help
```

---

## 🎯 Implementation Roadmap

### Phase 1: Foundation ✅
- Project structure, models, interfaces
- CLI framework
- Documentation

### Phase 2: Analyzer (Next)
Implement methods to capture:
- System info (hostname, OS, shell)
- Applications (Homebrew, App Store)
- Dotfiles & configs
- System preferences
- Shell environment

**Time estimate:** 8-12 hours

### Phase 3: Executor
Implement methods to apply:
- Install applications
- Copy dotfiles with conflict resolution
- Apply system preferences
- Update shell config
- Error handling & rollback

**Time estimate:** 8-12 hours

### Phase 4: Testing & Polish
- Unit tests for models
- Integration tests for Analyzer/Executor
- CLI improvements
- Error handling refinement

**Time estimate:** 4-6 hours

### Phase 5: TUI
- Interactive selection UI
- Progress monitoring
- Bubble Tea implementation

**Time estimate:** 8-10 hours

### Phase 6: GUI
- Tauri app scaffolding
- React frontend
- Device comparison
- Migration wizard

**Time estimate:** 12-16 hours

---

## 🔒 Security Considerations

✅ What Wave captures:
- Application names & versions
- Configuration file locations
- Public settings (Dock position, Finder view)
- Shell environment (public aliases)

❌ What Wave does NOT capture:
- Passwords or authentication tokens
- Private SSH keys
- API keys or secrets
- Financial data

**Best practice:** Review captured state before sharing or committing to Git

---

## 🤝 Contributing

1. Read DEVELOPMENT.md for setup
2. Check STATUS.md for next tasks
3. Follow PHASE2_GUIDE.md for implementation details
4. Test with `make test`
5. Keep code clean (`make fmt`)

---

## 🚀 Getting Started (Next Steps)

1. **Install Go** (if not already installed)
2. **Run `make build`** to compile
3. **Read PHASE2_GUIDE.md** to understand what needs implementing
4. **Start with AnalyzeDevice()** – it's the simplest
5. **Test incrementally** – run `make capture` after each method
6. **Use Git** to track progress

---

## 📖 Learning Resources

- Cobra CLI framework: https://cobra.dev
- Bubble Tea TUI: https://github.com/charmbracelet/bubbletea
- Go best practices: https://golang.org/doc/effective_go
- macOS system commands: man defaults, man brew

---

## 📝 Notes

- **Phase 2 (Analyzer)** is the critical path – implement this first
- **Dry-run mode** is essential for safety – always test before applying
- **YAML format** is recommended (human-readable, Git-friendly)
- **macOS-first** approach – Linux/Windows come later
- **Extensible design** – easy to add new categories or platforms

---

## ❓ FAQ

**Q: Why Go instead of Nix?**
A: Go compiles to native binaries, great CLI/TUI support, faster execution, easier testing.

**Q: Can I use this on other systems?**
A: Phase 1 is macOS-only. Linux/Windows support planned for later phases.

**Q: Is this secure?**
A: Never captures secrets. Review state files before sharing. Passwords remain encrypted in macOS keychain.

**Q: Can I git-commit my device state?**
A: Yes! Wave outputs YAML/JSON, making diffs easy. Just don't commit secrets.

**Q: How long until full MVP?**
A: ~40 hours of development across Phases 2-4. Could be 1-2 weeks with focused effort.

---

## 📞 Support

- Check STATUS.md for progress & next steps
- Read ARCHITECTURE.md for technical details
- See PHASE2_GUIDE.md for implementation help
- Review existing code examples in `internal/`

---

**Ready to build?** Start with `make build` and `make capture`!

🌊 Wave – Take your setup everywhere
