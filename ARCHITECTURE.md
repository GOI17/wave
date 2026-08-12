# Wave Architecture

## Overview

Wave is a modular device migration tool for macOS with multiple UI layers (CLI, TUI, GUI) sharing a common backend. The architecture separates concerns into analysis, execution, and orchestration.

```
┌─────────────────────────────────────────────────────────────┐
│ UI Layer (Presentation)                                     │
├─────────────────────┬──────────────┬────────────────────────┤
│   CLI (Cobra)       │ TUI (Bubble  │ GUI (Tauri + React)    │
│   + Commands        │ Tea)         │ + Components           │
└──────────┬──────────┴──────┬───────┴────────────┬───────────┘
           │                  │                    │
           └──────────────────┼────────────────────┘
                              │
┌─────────────────────────────▼────────────────────────────────┐
│ Orchestration Layer                                          │
├──────────────────────────────────────────────────────────────┤
│ Migrator                                                     │
│ ├─ Capture() - Analyze device state                         │
│ ├─ Apply() - Execute migration                              │
│ └─ Validate() - Check compatibility                         │
└─────────────────────────────────────────────────────────────┘
           │
           ├──────────────────────┬──────────────────────┐
           │                      │                      │
           ▼                      ▼                      ▼
┌──────────────────────┐  ┌─────────────────┐  ┌────────────────┐
│ Analyzer Interface   │  │ Executor        │  │ Models         │
│ (macOS impl)         │  │ Interface       │  │ (Data Structures)
│                      │  │ (macOS impl)    │  │                │
│ • AnalyzeDevice()    │  │                 │  │ • MigrationState
│ • AnalyzeApps()      │  │ • ExecuteApps()│  │ • DeviceInfo
│ • AnalyzeDotfiles()  │  │ • ExecuteFiles │  │ • AppGroup
│ • AnalyzePrefs()     │  │ • ExecutePrefs │  │ • DotfileGroup
│ • AnalyzeEnv()       │  │ • ExecuteEnv() │  │ • PrefsGroup
└──────────────────────┘  │ • Validate()   │  │ • EnvGroup
                          └─────────────────┘  │ • Task
                                               └────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────┐
│ Platform Layer (macOS-specific)                              │
├──────────────────────────────────────────────────────────────┤
│ • Homebrew CLI (`brew list`, `brew install`)                │
│ • App Store tools (`mas list`, `mas install`)               │
│ • System defaults (`defaults read/write`)                    │
│ • File system operations                                     │
│ • Shell environment parsing                                  │
│ • System info queries                                        │
└──────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Models (`internal/models/`)
**Responsibility:** Define data structures for migration state

- `MigrationState` - Complete snapshot of device configuration
- `DeviceInfo` - Host metadata
- `ApplicationGroup` - Homebrew, App Store, manual apps, extensions
- `DotfilesGroup` - Config files and directories
- `PreferencesGroup` - System preferences (Finder, Dock, Keyboard, etc)
- `EnvironmentGroup` - Shell configuration (aliases, functions, exports)
- `MigrationTask` - Individual action in migration workflow

### 2. Analyzer (`internal/analyzer/`)
**Responsibility:** Capture current device state

**Interface:**
```go
type Analyzer interface {
	AnalyzeDevice() (*MigrationState, error)
	AnalyzeApplications() (*ApplicationGroup, error)
	AnalyzeDotfiles() (*DotfilesGroup, error)
	AnalyzePreferences() (*PreferencesGroup, error)
	AnalyzeEnvironment() (*EnvironmentGroup, error)
}
```

**macOS Implementation Strategy:**
- Homebrew: Parse `brew list --json`, `brew tap`, `brew leaves`
- App Store: Use MAS CLI or System frameworks
- Dotfiles: Scan common locations, compute SHA256 hashes
- Preferences: Execute `defaults read` for each domain
- Environment: Parse shell config files (~/.zshrc, ~/.bashrc)
- System info: Use `system_profiler`, `uname`, environment variables

### 3. Executor (`internal/executor/`)
**Responsibility:** Apply migration state to target device

**Interface:**
```go
type Executor interface {
	ExecuteApplications(*ApplicationGroup, bool) ([]Task, error)
	ExecuteDotfiles(*DotfilesGroup, bool) ([]Task, error)
	ExecutePreferences(*PreferencesGroup, bool) ([]Task, error)
	ExecuteEnvironment(*EnvironmentGroup, bool) ([]Task, error)
	ValidateState(*MigrationState) error
}
```

**macOS Implementation Strategy:**
- **Dry-run:** Log commands without executing, show what would happen
- **Error handling:** Capture failures, continue on non-critical errors
- **Conflict resolution:** 
  - Backup existing files before overwriting
  - Prompt user for manual conflicts
  - Merge shell configs instead of replacing
- **Verification:** Checksum validation after copy, version checks for apps

### 4. Migrator (`internal/migrator/`)
**Responsibility:** Orchestrate capture and apply workflows

**Interface:**
```go
type Migrator interface {
	Capture(outputPath, format string) error
	Apply(inputPath string, dryRun bool, format string) error
}
```

**Workflow:**
1. Capture:
   - Call all analyzer methods
   - Combine results into MigrationState
   - Serialize to YAML/JSON
   
2. Apply:
   - Deserialize state from file
   - Validate target device compatibility
   - Execute each category
   - Report results

### 5. CLI (`ui/cli/`)
**Responsibility:** Cobra command structure and flag handling

**Commands:**
- `wave capture` - Export device state
- `wave apply` - Import and apply state
- `wave verify` - Check migration success
- `wave tui` - Launch TUI
- `wave gui` - Launch GUI
- `wave version` - Show version

### 6. TUI (`ui/tui/`)
**Responsibility:** Interactive terminal UI (Bubble Tea)

**Features:**
- Device selection (capture from/apply to)
- Category selection (choose what to migrate)
- Preview mode
- Progress monitoring
- Diff viewer

### 7. GUI (`ui/gui/`)
**Responsibility:** Desktop application (Tauri + React)

**Features:**
- Device comparison
- Visual configuration editor
- Migration wizard
- Settings management

## Data Flow

### Capture Flow
```
User Input
    ↓
CLI/TUI/GUI
    ↓
Migrator.Capture()
    ↓
Analyzer.Analyze*() ──→ System Queries (defaults, brew, etc)
    ↓
MigrationState built
    ↓
Serialize (YAML/JSON)
    ↓
File output
```

### Apply Flow
```
User Input (migration file)
    ↓
CLI/TUI/GUI
    ↓
Migrator.Apply()
    ↓
Deserialize state file
    ↓
Executor.Validate()
    ↓
Executor.Execute*() ──→ System Commands (defaults write, brew install, etc)
    ↓
MigrationTask results
    ↓
Success/Failure report
```

## Serialization Format

### YAML Example
```yaml
version: "1.0.0"
created_at: 2025-01-15T10:30:00Z
device_info:
  hostname: MacBook-Pro
  username: goldeni
  os_version: "15.1"
  shell: /bin/zsh
applications:
  homebrew:
    - name: git
      type: formula
      version: "2.45.0"
    - name: iterm2
      type: cask
      version: "3.4.0"
dotfiles:
  files:
    - source: ~/.zshrc
      destination: ~/.zshrc
      checksum: "abc123..."
preferences:
  finder:
    show_hidden_files: true
  dock:
    position: left
    autohide: true
environment:
  shell: /bin/zsh
  shell_profile: ~/.zshrc
  env_vars:
    EDITOR: nvim
  aliases:
    ll: ls -la
```

## Extensibility Points

1. **Add new analysis module** - Implement Analyzer interface
2. **Add new executor** - Implement Executor interface  
3. **Add new preference type** - Extend PreferencesGroup model
4. **Add new UI** - Use Migrator API with new framework
5. **Custom hooks** - Pre/post migration scripts in yaml

## Dependencies

- **Cobra** - CLI framework
- **Bubble Tea** - TUI framework
- **YAML/JSON** - Serialization
- **Tauri** - Desktop GUI (future)

## Testing Strategy

- Unit tests for models & serialization
- Integration tests for analyzer (mock system commands)
- Executor tests (dry-run mode validation)
- CLI tests (command parsing & error handling)
- End-to-end tests (capture → apply → verify)

## Future Enhancements

1. Cloud sync (backup/restore to GitHub, iCloud)
2. Version control integration
3. Team/shared profiles
4. Linux/Windows support
5. Mobile device support
6. Incremental/delta migrations
7. Scheduled automatic backups
8. Web-based UI
