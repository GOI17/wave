# 🌊 Wave – macOS Device Migrator

**A comprehensive tool to replicate your entire macOS device setup with CLI, TUI, and GUI interfaces.**

[![Status](https://img.shields.io/badge/version-1.0.0-blue.svg)](https://github.com/yourusername/wave/releases/tag/v1.0.0)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.23-00ADD8.svg)](https://golang.org)

---

## ✨ Features

**Wave** enables you to capture your complete macOS device configuration and replicate it on any other Mac. Perfect for new machine setup, team standardization, or disaster recovery.

### What You Can Migrate

- 📦 **Applications** – Homebrew formulas, casks, App Store apps, VS Code extensions
- 📝 **Dotfiles** – Shell configs, editor settings, app configurations
- ⚙️ **System Preferences** – Finder, Dock, Keyboard, Trackpad, and more
- 🔧 **Environment** – Aliases, functions, exports, PATH configuration

### Three Interfaces

- **CLI** – Full-featured command-line for scripting
- **TUI** – Interactive terminal UI with menu navigation
- **GUI** – Web-based interface accessible from browser

### Safety First

- ✅ **Dry-run Mode** – Preview changes before applying
- ✅ **Automatic Backups** – Backs up existing configs
- ✅ **Verification** – Validates device compatibility
- ✅ **Checksum Validation** – Ensures file integrity

---

## 🚀 Quick Start

### Installation

```bash
# Clone and build
git clone https://github.com/yourusername/wave.git
cd wave
make build

# Or install from binaries
curl -L https://github.com/yourusername/wave/releases/download/v1.0.0/wave-v1.0.0.tar.gz | tar xz
sudo cp wave /usr/local/bin/
```

### Basic Usage

```bash
# Capture your device
wave capture --output my-setup.yaml

# On another Mac, preview the migration
wave apply --input my-setup.yaml --dry-run

# Apply the migration
wave apply --input my-setup.yaml

# Or use interactive TUI
wave tui

# Or open web GUI
wave gui
```

---

## 📋 Commands

```bash
# Capture device state
wave capture [--output FILE] [--format yaml|json]

# Apply captured state
wave apply [--input FILE] [--dry-run] [--format yaml|json]

# Verify migration
wave verify --input FILE

# Compare states
wave diff STATE1 STATE2

# Interactive terminal UI
wave tui

# Web-based GUI
wave gui [--port 8080]

# Show version
wave version

# Show help
wave --help
```

---

## 💻 Use Cases

### New Machine Setup

```bash
# On your old Mac
old-mac$ wave capture --output setup.yaml

# Transfer to new Mac
scp setup.yaml newmac:~

# On new Mac
new-mac$ wave apply --input setup.yaml --dry-run
new-mac$ wave apply --input setup.yaml
```

### Team Standardization

```bash
# Create team standard
wave capture --output team-standard.yaml

# Commit to Git
git add team-standard.yaml
git commit -m "Team development environment"

# Team members apply
wave apply --input team-standard.yaml --dry-run
wave apply --input team-standard.yaml
```

### Disaster Recovery

```bash
# Weekly backup
0 0 * * * wave capture --output ~/backups/wave-$(date +\%Y\%m\%d).yaml

# Quick restore
wave apply --input ~/backups/wave-20250115.yaml
```

---

## 🔒 Security

### Wave Captures

✅ Application names and versions  
✅ Configuration files (dotfiles)  
✅ System preferences  
✅ Shell aliases and functions  
✅ Public SSH keys (not private keys)

### Wave Does NOT Capture

❌ Passwords or tokens  
❌ Private SSH keys  
❌ API keys  
❌ Browser saved passwords  
❌ Secure credentials

**Recommendation:** Review captured files before sharing. Never commit state files with sensitive data to public repositories.

---

## 📈 Performance

- **Capture time:** 2-8 seconds (depending on system)
- **State file size:** 50KB-500KB
- **Memory usage:** ~50MB CLI, ~80MB TUI, ~40MB GUI
- **Dry-run:** <1 second

---

## 🧪 Testing

```bash
# Run all tests
make test

# With coverage
make coverage

# With race detection
make test-race

# Benchmarks
make benchmark
```

See [TESTING.md](TESTING.md) for comprehensive testing guide.

---

## 📦 Requirements

### Minimum

- macOS 10.14+
- Go 1.23+ (for building)

### Recommended

- Homebrew (for app management)
- VS Code (for extension capture)
- `mas` cli-tool (for App Store integration)

---

## 🏗️ Architecture

### Three-Tier Design

```
UI Layer (CLI / TUI / GUI)
        ↓
Orchestration (Migrator)
        ↓
Core Logic (Analyzer / Executor)
        ↓
System APIs (defaults, brew, etc)
```

### Key Components

- **Analyzer** – Captures device state via system commands
- **Executor** – Applies state to target device
- **Migrator** – Orchestrates workflows
- **CLI** – Cobra command framework
- **TUI** – Bubble Tea terminal UI
- **GUI** – Web server with HTML/CSS/JS

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed design documentation.

---

## 📊 Formats Supported

### YAML (Default)

Human-readable, Git-friendly format:

```yaml
version: "1.0.0"
created_at: 2025-01-15T10:30:00Z
device_info:
  hostname: MacBook-Pro
  username: goldeni
  os_version: "15.1"
applications:
  homebrew:
    - name: git
      type: formula
      version: "2.45.0"
```

### JSON

Programmatic access and integration:

```json
{
  "version": "1.0.0",
  "created_at": "2025-01-15T10:30:00Z",
  "device_info": {
    "hostname": "MacBook-Pro"
  }
}
```

---

## 🐳 Docker Support

```bash
# Build Docker image
docker build -t wave:latest .

# Run in container
docker run -it wave:latest capture --help

# Interactive mode
docker run -it wave:latest tui
```

---

## 📚 Documentation

- **[QUICKSTART.md](QUICKSTART.md)** – Get started in 5 minutes
- **[ARCHITECTURE.md](ARCHITECTURE.md)** – Technical design and internals
- **[DEVELOPMENT.md](DEVELOPMENT.md)** – Development setup guide
- **[TESTING.md](TESTING.md)** – Comprehensive testing procedures
- **[RELEASE_NOTES.md](RELEASE_NOTES.md)** – Full v1.0.0 release details

---

## 🤝 Contributing

Wave is open source and welcomes contributions!

```bash
# Setup development environment
git clone https://github.com/yourusername/wave.git
cd wave
make build test

# Create feature branch
git checkout -b feature/your-feature

# Make changes and test
make fmt
make test
make lint

# Push and create PR
git push origin feature/your-feature
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed contribution guidelines.

---

## 📝 License

MIT License – See [LICENSE](LICENSE) file

---

## 🙏 Acknowledgments

Built with:
- [Cobra](https://cobra.dev) – CLI framework
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) – Terminal UI
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) – Terminal styling
- [Viper](https://github.com/spf13/viper) – Configuration

---

## 📞 Support

- **Issues:** [GitHub Issues](https://github.com/yourusername/wave/issues)
- **Discussions:** [GitHub Discussions](https://github.com/yourusername/wave/discussions)
- **Security:** security@example.com

---

## 🎯 Roadmap

### v1.0.0 ✅ (Current)

- ✓ CLI, TUI, GUI interfaces
- ✓ Full device capture
- ✓ Safe migration with dry-run
- ✓ YAML/JSON formats
- ✓ Comprehensive testing

### v1.1.0 (Q2 2025)

- [ ] Linux support
- [ ] Desktop app (native)
- [ ] Team profiles
- [ ] Enhanced diff viewer

### v1.2.0 (Q3 2025)

- [ ] Cloud backup/restore
- [ ] Incremental migrations
- [ ] Migration history
- [ ] Advanced analytics

### v2.0.0 (Q4 2025)

- [ ] Windows support
- [ ] Multi-device orchestration
- [ ] Mobile support
- [ ] Enterprise features

---

## 🌟 Stars & Support

If Wave helps you, please:
- ⭐ Star on [GitHub](https://github.com/yourusername/wave)
- 📢 Share with others
- 🐛 Report issues
- 🤝 Contribute improvements

---

**🌊 Wave v1.0.0** – Take your setup everywhere
