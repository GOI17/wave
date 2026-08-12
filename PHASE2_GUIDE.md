# Phase 2 Implementation Guide – Analyzer

This document provides implementation guidance for the Analyzer phase. Each section includes:
1. What to implement
2. Key functions/commands
3. Implementation hints
4. Expected output

---

## AnalyzeDevice() – System Information

**Goal:** Capture hostname, username, OS version, architecture, and shell

### Key System Commands

```bash
# Hostname
hostname                    # Returns: MacBook-Pro

# Username
whoami                      # Returns: goldeni
fullname=$(dscl . read /Users/$USER RealName | sed 's/^[^:]*: //')

# OS Version
sw_vers -productVersion    # Returns: 15.1

# Architecture
uname -m                    # Returns: arm64

# Default Shell
echo $SHELL                 # Returns: /bin/zsh
```

### Implementation Pattern

```go
func (a *macOSAnalyzer) AnalyzeDevice() (*models.MigrationState, error) {
    hostname, err := os.Hostname()
    user, err := user.Current()
    
    // Use exec.Command to run system commands
    cmd := exec.Command("sw_vers", "-productVersion")
    osVersion, err := cmd.CombinedOutput()
    
    // Build and return state
    state := &models.MigrationState{
        Version: "1.0.0",
        CreatedAt: time.Now(),
        SourceDevice: models.DeviceInfo{
            Hostname: hostname,
            Username: user.Username,
            FullName: getFullName(), // Shell out to dscl
            OSVersion: string(osVersion),
            Architecture: runtime.GOARCH,
            Shell: os.Getenv("SHELL"),
        },
    }
    return state, nil
}
```

---

## AnalyzeApplications() – Homebrew & Apps

**Goal:** Capture installed Homebrew formulas, casks, and App Store apps

### Key System Commands

```bash
# Homebrew formulas (JSON output)
brew list --formula --json      # Returns: [{"name": "git", "version": "2.45.0"}, ...]

# Homebrew casks
brew list --casks --json        # Returns: [{"name": "iterm2", ...}, ...]

# Homebrew taps
brew tap --json                 # Returns: {...}

# App Store apps (requires 'mas' tool)
mas list                         # Returns: "409183694 Keynote (14.0)"

# VS Code extensions
code --list-extensions          # Returns: extensions per line
```

### Implementation Pattern

```go
func (a *macOSAnalyzer) AnalyzeApplications() (*models.ApplicationGroup, error) {
    apps := &models.ApplicationGroup{
        Homebrew: []models.HomebrewPackage{},
        AppStore: []models.AppStoreApp{},
        Manual: []models.ManualApp{},
    }
    
    // Capture Homebrew formulas
    formulas, err := parseHomebrewJSON("brew list --formula --json")
    apps.Homebrew = append(apps.Homebrew, formulas...)
    
    // Capture Homebrew casks
    casks, err := parseHomebrewJSON("brew list --casks --json")
    apps.Homebrew = append(apps.Homebrew, casks...)
    
    // Capture VS Code extensions
    vsCode, err := parseVSCodeExtensions()
    apps.VSCodeExtensions = vsCode
    
    return apps, nil
}

func parseHomebrewJSON(cmdStr string) ([]models.HomebrewPackage, error) {
    cmd := exec.Command("bash", "-c", cmdStr)
    output, err := cmd.Output()
    
    var result []map[string]interface{}
    json.Unmarshal(output, &result)
    
    // Convert to HomebrewPackage structs
    return packages, nil
}
```

---

## AnalyzeDotfiles() – Configuration Files

**Goal:** Find and hash dotfiles in common locations

### Key Directories

```bash
~/.zshrc
~/.bashrc
~/.bash_profile
~/.zsh_profile
~/.config/
~/.ssh/
~/.ssh/config
~/.gitconfig
~/.git-credentials
~/.vim/
~/.vimrc
~/.nvim/
~/.tmux.conf
~/.iterm2/
```

### Implementation Pattern

```go
func (a *macOSAnalyzer) AnalyzeDotfiles() (*models.DotfilesGroup, error) {
    dotfiles := &models.DotfilesGroup{
        Files: []models.DotfileEntry{},
        Directories: []models.DirEntry{},
    }
    
    commonDotfiles := []string{
        ".zshrc", ".bashrc", ".gitconfig", ".vimrc", ".tmux.conf",
    }
    
    for _, dotfile := range commonDotfiles {
        path := filepath.Join(a.homeDir, dotfile)
        if _, err := os.Stat(path); err == nil {
            // File exists, compute checksum
            checksum, err := computeSHA256(path)
            dotfiles.Files = append(dotfiles.Files, models.DotfileEntry{
                Source: path,
                Destination: path,
                Checksum: checksum,
            })
        }
    }
    
    // Scan .config directory recursively
    configPath := filepath.Join(a.homeDir, ".config")
    err := filepath.Walk(configPath, func(path string, info os.FileInfo, err error) error {
        if !info.IsDir() {
            checksum, _ := computeSHA256(path)
            dotfiles.Files = append(dotfiles.Files, models.DotfileEntry{
                Source: path,
                Destination: path,
                Checksum: checksum,
            })
        }
        return nil
    })
    
    return dotfiles, nil
}

func computeSHA256(filePath string) (string, error) {
    file, _ := os.Open(filePath)
    defer file.Close()
    hash := sha256.New()
    io.Copy(hash, file)
    return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
```

---

## AnalyzePreferences() – System Settings

**Goal:** Capture Finder, Dock, Keyboard, and other system preferences

### Key `defaults` Domains

```bash
# Finder preferences
defaults read com.apple.finder

# Dock preferences
defaults read com.apple.dock

# Keyboard preferences
defaults read -g com.apple.keyboard
defaults read -g ApplePressAndHoldEnabled

# Trackpad preferences
defaults read com.apple.trackpad

# System preferences
defaults read NSGlobalDomain
```

### Implementation Pattern

```go
func (a *macOSAnalyzer) AnalyzePreferences() (*models.PreferencesGroup, error) {
    prefs := &models.PreferencesGroup{
        Apps: make(map[string]interface{}),
    }
    
    // Finder preferences
    finderPrefs, err := readDefaults("com.apple.finder", []string{
        "AppleShowAllFiles",
        "FXPreferredViewStyle",
    })
    prefs.Finder.ShowHiddenFiles = asBool(finderPrefs["AppleShowAllFiles"])
    prefs.Finder.DefaultViewMode = asString(finderPrefs["FXPreferredViewStyle"])
    
    // Dock preferences
    dockPrefs, err := readDefaults("com.apple.dock", []string{
        "orientation",
        "autohide",
        "show-recents",
        "persistent-apps",
    })
    prefs.Dock.Position = asString(dockPrefs["orientation"])
    prefs.Dock.Autohide = asBool(dockPrefs["autohide"])
    
    // Keyboard preferences
    keyboardPrefs, err := readDefaults("-g", []string{
        "KeyRepeat",
        "InitialKeyRepeat",
    })
    prefs.Keyboard.KeyRepeat = asInt(keyboardPrefs["KeyRepeat"])
    prefs.Keyboard.InitialRepeat = asInt(keyboardPrefs["InitialKeyRepeat"])
    
    return prefs, nil
}

func readDefaults(domain string, keys []string) (map[string]interface{}, error) {
    result := make(map[string]interface{})
    for _, key := range keys {
        cmd := exec.Command("defaults", "read", domain, key)
        output, err := cmd.Output()
        if err == nil {
            result[key] = string(output)
        }
    }
    return result, nil
}
```

---

## AnalyzeEnvironment() – Shell Config

**Goal:** Extract shell aliases, functions, environment variables, and exports

### Key Files

```bash
~/.zshrc       # Main Zsh config
~/.bashrc      # Main Bash config
~/.zsh_profile # Login shell (older systems)
~/.bash_profile # Login shell (older systems)
~/.zsh_aliases # Zsh aliases (some systems)
```

### Implementation Pattern

```go
func (a *macOSAnalyzer) AnalyzeEnvironment() (*models.EnvironmentGroup, error) {
    env := &models.EnvironmentGroup{
        Shell: os.Getenv("SHELL"),
        EnvironmentVars: make(map[string]string),
        Aliases: make(map[string]string),
        Functions: make(map[string]string),
    }
    
    // Detect shell config file
    configFile := filepath.Join(a.homeDir, ".zshrc")
    if _, err := os.Stat(configFile); os.IsNotExist(err) {
        configFile = filepath.Join(a.homeDir, ".bashrc")
    }
    env.ShellProfile = configFile
    
    // Parse shell config file
    content, _ := os.ReadFile(configFile)
    lines := strings.Split(string(content), "\n")
    
    for _, line := range lines {
        line = strings.TrimSpace(line)
        
        // Skip comments and empty lines
        if strings.HasPrefix(line, "#") || line == "" {
            continue
        }
        
        // Extract aliases: alias ll='ls -la'
        if strings.HasPrefix(line, "alias ") {
            parts := strings.Split(strings.TrimPrefix(line, "alias "), "=")
            if len(parts) == 2 {
                key := strings.TrimSpace(parts[0])
                value := strings.TrimSpace(strings.Trim(parts[1], "'\""))
                env.Aliases[key] = value
            }
        }
        
        // Extract exports: export PATH=...
        if strings.HasPrefix(line, "export ") {
            parts := strings.Split(strings.TrimPrefix(line, "export "), "=")
            if len(parts) == 2 {
                key := strings.TrimSpace(parts[0])
                value := strings.TrimSpace(parts[1])
                env.EnvironmentVars[key] = value
            }
        }
    }
    
    // Detect Homebrew
    if _, err := os.Stat("/usr/local/Homebrew"); err == nil {
        env.Homebrew.Prefix = "/usr/local"
    } else if _, err := os.Stat("/opt/homebrew"); err == nil {
        env.Homebrew.Prefix = "/opt/homebrew"
    }
    
    return env, nil
}
```

---

## Testing Hints

After implementing each method, test with:

```bash
# Capture and inspect output
make capture
cat state.yaml

# Compare with manual commands
defaults read com.apple.finder AppleShowAllFiles
brew list --formula --json | jq .

# Validate checksums
shasum -a 256 ~/.zshrc
```

---

## Common Utilities

Create helper functions in `internal/analyzer/helpers.go`:

```go
// Execute shell command and return output
func execCommand(name string, args ...string) (string, error)

// Convert string to bool, int, etc
func asBool(v interface{}) bool
func asInt(v interface{}) int
func asString(v interface{}) string

// SHA256 hash of file
func computeFileHash(path string) (string, error)

// Parse JSON from command output
func parseJSON(data []byte, v interface{}) error
```

---

## What NOT to Do

❌ Don't capture sensitive data (passwords, tokens, API keys)
❌ Don't capture system files outside user's home directory (except /usr/local/Homebrew)
❌ Don't make assumptions about shell (detect dynamically)
❌ Don't fail on missing optional tools (mas, code)
❌ Don't run `sudo` without user confirmation

---

## Success Criteria

After implementation:
- ✅ `wave capture` generates valid YAML/JSON
- ✅ All fields in MigrationState are populated
- ✅ File checksums are consistent
- ✅ No errors for standard macOS systems
- ✅ Graceful handling of missing optional tools
