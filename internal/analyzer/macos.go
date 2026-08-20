package analyzer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"wave/internal/models"
)

// macOSAnalyzer implements Analyzer for macOS
type macOSAnalyzer struct {
	homeDir string
}

// NewMacOSAnalyzer creates a new macOS analyzer
func NewMacOSAnalyzer(homeDir string) Analyzer {
	return &macOSAnalyzer{
		homeDir: homeDir,
	}
}

// AnalyzeDevice captures device information
func (a *macOSAnalyzer) AnalyzeDevice() (*models.MigrationState, error) {
	hostname, _ := os.Hostname()
	osVersion := a.getOSVersion()
	arch := runtime.GOARCH
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	currentUser, _ := user.Current()
	username := currentUser.Username
	fullName := a.getFullName(username)

	state := &models.MigrationState{
		Version:   "1.0.0",
		CreatedAt: time.Now(),
		SourceDevice: models.DeviceInfo{
			Hostname:     hostname,
			Username:     username,
			FullName:     fullName,
			OSVersion:    osVersion,
			Architecture: arch,
			Shell:        shell,
		},
	}

	return state, nil
}

// AnalyzeApplications captures installed applications
func (a *macOSAnalyzer) AnalyzeApplications() (*models.ApplicationGroup, error) {
	apps := &models.ApplicationGroup{
		Homebrew:         []models.HomebrewPackage{},
		AppStore:         []models.AppStoreApp{},
		Manual:           []models.ManualApp{},
		VSCodeExtensions: []string{},
	}

	// Capture Homebrew formulas
	formulas, _ := a.getHomebrewFormulas()
	apps.Homebrew = append(apps.Homebrew, formulas...)

	// Capture Homebrew casks
	casks, _ := a.getHomebrewCasks()
	apps.Homebrew = append(apps.Homebrew, casks...)

	// Capture VS Code extensions
	extensions, _ := a.getVSCodeExtensions()
	apps.VSCodeExtensions = extensions

	// Capture App Store apps (optional, requires mas)
	masApps, _ := a.getMasApps()
	apps.AppStore = masApps

	// Manual apps have no portable install payload, but retain enough metadata
	// to report what still needs to be installed on the target Mac.
	apps.Manual = a.getManualApps(apps)

	return apps, nil
}

// AnalyzeDotfiles captures dotfiles and config files
func (a *macOSAnalyzer) AnalyzeDotfiles() (*models.DotfilesGroup, error) {
	dotfiles := &models.DotfilesGroup{
		Files:       []models.DotfileEntry{},
		Directories: []models.DirEntry{},
	}

	commonDotfiles := []string{
		".zshrc", ".bashrc", ".bash_profile", ".zsh_profile",
		".gitconfig", ".vimrc", ".tmux.conf", ".editorconfig",
		".prettierrc", ".eslintrc.json", ".gitignore_global",
	}

	// Scan common dotfiles
	for _, dotfile := range commonDotfiles {
		path := filepath.Join(a.homeDir, dotfile)
		if _, err := os.Stat(path); err == nil {
			checksum, _ := a.computeSHA256(path)
			dotfiles.Files = append(dotfiles.Files, models.DotfileEntry{
				Source:      path,
				Destination: path,
				Checksum:    checksum,
			})
		}
	}

	// Scan .config directory recursively
	configPath := filepath.Join(a.homeDir, ".config")
	if _, err := os.Stat(configPath); err == nil {
		a.scanDirectory(configPath, dotfiles, 0)
	}

	// Scan .ssh (without private keys)
	sshPath := filepath.Join(a.homeDir, ".ssh")
	if _, err := os.Stat(sshPath); err == nil {
		a.scanDirectoryExcluding(sshPath, []string{"id_", "known_hosts"}, dotfiles)
	}

	return dotfiles, nil
}

// AnalyzePreferences captures system preferences
func (a *macOSAnalyzer) AnalyzePreferences() (*models.PreferencesGroup, error) {
	prefs := &models.PreferencesGroup{
		Apps: make(map[string]interface{}),
	}

	// Finder preferences
	finderPrefs := a.readDefaults("com.apple.finder", []string{
		"AppleShowAllFiles",
		"FXPreferredViewStyle",
	})
	prefs.Finder = models.FinderPrefs{
		ShowHiddenFiles: a.asBool(finderPrefs["AppleShowAllFiles"]),
		DefaultViewMode: a.asString(finderPrefs["FXPreferredViewStyle"]),
	}

	// Dock preferences
	dockPrefs := a.readDefaults("com.apple.dock", []string{
		"orientation",
		"autohide",
		"show-recents",
	})
	prefs.Dock = models.DockPrefs{
		Position:    a.asString(dockPrefs["orientation"]),
		Autohide:    a.asBool(dockPrefs["autohide"]),
		ShowRecents: a.asBool(dockPrefs["show-recents"]),
	}

	// Keyboard preferences
	keyboardPrefs := a.readDefaults("-g", []string{
		"KeyRepeat",
		"InitialKeyRepeat",
	})
	prefs.Keyboard = models.KeyboardPrefs{
		KeyRepeat:     a.asInt(keyboardPrefs["KeyRepeat"]),
		InitialRepeat: a.asInt(keyboardPrefs["InitialKeyRepeat"]),
	}

	// System preferences
	prefs.System = models.SystemPrefs{
		ComputerName: a.getComputerName(),
		TimeZone:     a.getTimeZone(),
		Language:     a.getLanguage(),
	}

	return prefs, nil
}

// AnalyzeEnvironment captures shell environment
func (a *macOSAnalyzer) AnalyzeEnvironment() (*models.EnvironmentGroup, error) {
	env := &models.EnvironmentGroup{
		EnvironmentVars: make(map[string]string),
		Aliases:         make(map[string]string),
		Functions:       make(map[string]string),
		Homebrew:        models.HomebrewEnv{},
	}

	env.Shell = os.Getenv("SHELL")
	if env.Shell == "" {
		env.Shell = "/bin/zsh"
	}

	// Detect shell profile file
	shellProfile := a.detectShellProfile()
	env.ShellProfile = shellProfile

	// Parse shell config
	a.parseShellConfig(shellProfile, env)

	// Detect Homebrew
	if _, err := os.Stat("/opt/homebrew"); err == nil {
		env.Homebrew.Prefix = "/opt/homebrew"
	} else if _, err := os.Stat("/usr/local/Homebrew"); err == nil {
		env.Homebrew.Prefix = "/usr/local"
	}

	// Get Node version
	if nodeVersion, err := a.execCommand("node", "--version"); err == nil {
		env.NodeVersion = strings.TrimSpace(nodeVersion)
	}

	// Get Python version
	if pyVersion, err := a.execCommand("python3", "--version"); err == nil {
		env.PythonVersion = strings.TrimSpace(pyVersion)
	}

	return env, nil
}

// Helper methods

func (a *macOSAnalyzer) getOSVersion() string {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output))
}

func (a *macOSAnalyzer) getFullName(username string) string {
	cmd := exec.Command("id", "-F", username)
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output))
}

func (a *macOSAnalyzer) getHomebrewFormulas() ([]models.HomebrewPackage, error) {
	cmd := exec.Command("brew", "leaves")
	output, err := cmd.Output()
	if err != nil {
		return []models.HomebrewPackage{}, nil
	}
	packages := []models.HomebrewPackage{}
	for _, name := range strings.Fields(string(output)) {
		version, _ := a.execCommand("brew", "list", "--versions", name)
		packages = append(packages, models.HomebrewPackage{
			Name:    name,
			Type:    "formula",
			Version: version,
		})
	}
	return packages, nil
}

func (a *macOSAnalyzer) getHomebrewCasks() ([]models.HomebrewPackage, error) {
	cmd := exec.Command("bash", "-c", "brew list --casks --json 2>/dev/null || echo '[]'")
	output, _ := cmd.Output()

	var result []map[string]interface{}
	json.Unmarshal(output, &result)

	packages := []models.HomebrewPackage{}
	for _, pkg := range result {
		packages = append(packages, models.HomebrewPackage{
			Name:    a.asString(pkg["name"]),
			Type:    "cask",
			Version: a.asString(pkg["version"]),
		})
	}
	return packages, nil
}

func (a *macOSAnalyzer) getVSCodeExtensions() ([]string, error) {
	cmd := exec.Command("bash", "-c", "code --list-extensions 2>/dev/null")
	output, _ := cmd.Output()

	extensions := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			extensions = append(extensions, line)
		}
	}
	return extensions, nil
}

func (a *macOSAnalyzer) getMasApps() ([]models.AppStoreApp, error) {
	cmd := exec.Command("bash", "-c", "mas list 2>/dev/null")
	output, _ := cmd.Output()

	apps := []models.AppStoreApp{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			apps = append(apps, models.AppStoreApp{
				BundleID: parts[0],
				Name:     strings.Join(parts[1:len(parts)-1], " "),
				Version:  parts[len(parts)-1],
			})
		}
	}
	return apps, nil
}

func (a *macOSAnalyzer) getManualApps(managed *models.ApplicationGroup) []models.ManualApp {
	managedBundleIDs := make(map[string]bool)
	managedNames := make(map[string]bool)
	for _, app := range managed.AppStore {
		managedBundleIDs[app.BundleID] = true
		managedNames[normalizeAppName(app.Name)] = true
	}
	for _, app := range managed.Homebrew {
		if app.Type == "cask" {
			managedNames[normalizeAppName(app.Name)] = true
		}
	}
	return a.scanManualApps([]string{"/Applications", filepath.Join(a.homeDir, "Applications")}, managedBundleIDs, managedNames)
}

func (a *macOSAnalyzer) scanManualApps(directories []string, managedBundleIDs, managedNames map[string]bool) []models.ManualApp {
	var apps []models.ManualApp
	seen := make(map[string]bool)
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if seen[path] {
				continue
			}
			seen[path] = true
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			version, _ := a.execCommand("defaults", "read", filepath.Join(path, "Contents", "Info"), "CFBundleShortVersionString")
			bundleID, _ := a.execCommand("defaults", "read", filepath.Join(path, "Contents", "Info"), "CFBundleIdentifier")
			if managedBundleIDs[bundleID] || managedNames[normalizeAppName(name)] {
				continue
			}
			apps = append(apps, models.ManualApp{Name: name, Path: path, Version: version, BundleID: bundleID})
		}
	}
	return apps
}

func normalizeAppName(value string) string {
	value = strings.ToLower(value)
	return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(value)
}

func (a *macOSAnalyzer) scanDirectory(path string, dotfiles *models.DotfilesGroup, depth int) {
	if depth > 3 {
		return
	}

	entries, _ := os.ReadDir(path)
	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())

		if entry.IsDir() {
			a.scanDirectory(fullPath, dotfiles, depth+1)
		} else {
			checksum, _ := a.computeSHA256(fullPath)
			dotfiles.Files = append(dotfiles.Files, models.DotfileEntry{
				Source:      fullPath,
				Destination: fullPath,
				Checksum:    checksum,
			})
		}
	}
}

func (a *macOSAnalyzer) scanDirectoryExcluding(path string, exclude []string, dotfiles *models.DotfilesGroup) {
	entries, _ := os.ReadDir(path)
	for _, entry := range entries {
		skip := false
		for _, excl := range exclude {
			if strings.Contains(entry.Name(), excl) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		if !entry.IsDir() {
			checksum, _ := a.computeSHA256(fullPath)
			dotfiles.Files = append(dotfiles.Files, models.DotfileEntry{
				Source:      fullPath,
				Destination: fullPath,
				Checksum:    checksum,
			})
		}
	}
}

func (a *macOSAnalyzer) readDefaults(domain string, keys []string) map[string]interface{} {
	result := make(map[string]interface{})
	for _, key := range keys {
		cmd := exec.Command("defaults", "read", domain, key)
		output, _ := cmd.Output()
		result[key] = strings.TrimSpace(string(output))
	}
	return result
}

func (a *macOSAnalyzer) detectShellProfile() string {
	shells := []string{
		filepath.Join(a.homeDir, ".zshrc"),
		filepath.Join(a.homeDir, ".bashrc"),
		filepath.Join(a.homeDir, ".bash_profile"),
		filepath.Join(a.homeDir, ".zsh_profile"),
	}

	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}
	return filepath.Join(a.homeDir, ".zshrc")
}

func (a *macOSAnalyzer) parseShellConfig(configPath string, env *models.EnvironmentGroup) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// Extract aliases: alias ll='ls -la'
		if strings.HasPrefix(line, "alias ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "alias "), "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
				env.Aliases[key] = value
			}
		}

		// Extract exports: export PATH=...
		if strings.HasPrefix(line, "export ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "export "), "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				env.EnvironmentVars[key] = value
			}
		}
	}
}

func (a *macOSAnalyzer) getComputerName() string {
	cmd := exec.Command("scutil", "--get", "ComputerName")
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output))
}

func (a *macOSAnalyzer) getTimeZone() string {
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	const marker = "/zoneinfo/"
	if index := strings.Index(target, marker); index >= 0 {
		return target[index+len(marker):]
	}
	return ""
}

func (a *macOSAnalyzer) getLanguage() string {
	cmd := exec.Command("defaults", "read", "-g", "AppleLanguages")
	output, _ := cmd.Output()
	return normalizeCapturedLanguage(string(output))
}

func normalizeCapturedLanguage(output string) string {
	value := strings.TrimSpace(output)
	value = strings.Trim(value, "() \n\t")
	var languages []string
	for _, language := range strings.Split(value, ",") {
		language = strings.Trim(strings.TrimSpace(language), `"`)
		if language != "" {
			languages = append(languages, language)
		}
	}
	return strings.Join(languages, ",")
}

func (a *macOSAnalyzer) computeSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (a *macOSAnalyzer) execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func (a *macOSAnalyzer) asBool(v interface{}) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s == "1" || strings.ToLower(s) == "true"
	}
	return false
}

func (a *macOSAnalyzer) asInt(v interface{}) int {
	if v == nil {
		return 0
	}
	if s, ok := v.(string); ok {
		var i int
		fmt.Sscanf(s, "%d", &i)
		return i
	}
	return 0
}

func (a *macOSAnalyzer) asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
