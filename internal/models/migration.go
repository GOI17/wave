package models

import "time"

// MigrationState represents a complete device snapshot for migration
type MigrationState struct {
	Version      string           `yaml:"version" json:"version"`
	CreatedAt    time.Time        `yaml:"created_at" json:"created_at"`
	SourceDevice DeviceInfo       `yaml:"device_info" json:"device_info"`
	Applications ApplicationGroup `yaml:"applications" json:"applications"`
	Dotfiles     DotfilesGroup    `yaml:"dotfiles" json:"dotfiles"`
	Preferences  PreferencesGroup `yaml:"preferences" json:"preferences"`
	Environment  EnvironmentGroup `yaml:"environment" json:"environment"`
}

// DeviceInfo captures source device metadata
type DeviceInfo struct {
	Hostname    string `yaml:"hostname" json:"hostname"`
	Username    string `yaml:"username" json:"username"`
	FullName    string `yaml:"full_name" json:"full_name"`
	OSVersion   string `yaml:"os_version" json:"os_version"`
	Architecture string `yaml:"architecture" json:"architecture"`
	Shell       string `yaml:"shell" json:"shell"`
}

// ApplicationGroup groups application management
type ApplicationGroup struct {
	Homebrew     []HomebrewPackage `yaml:"homebrew" json:"homebrew"`
	AppStore     []AppStoreApp     `yaml:"app_store" json:"app_store"`
	Manual       []ManualApp       `yaml:"manual" json:"manual"`
	VSCodeExtensions []string      `yaml:"vscode_extensions" json:"vscode_extensions"`
}

// HomebrewPackage represents a Homebrew formula/cask
type HomebrewPackage struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"` // formula, cask, tap
	Version  string `yaml:"version" json:"version"`
	Pinned   bool   `yaml:"pinned" json:"pinned"`
}

// AppStoreApp represents an App Store application
type AppStoreApp struct {
	BundleID string `yaml:"bundle_id" json:"bundle_id"`
	Name     string `yaml:"name" json:"name"`
	Version  string `yaml:"version" json:"version"`
}

// ManualApp represents manually installed apps
type ManualApp struct {
	Name     string `yaml:"name" json:"name"`
	Path     string `yaml:"path" json:"path"`
	Version  string `yaml:"version" json:"version"`
}

// DotfilesGroup groups dotfiles and configuration
type DotfilesGroup struct {
	Files       []DotfileEntry `yaml:"files" json:"files"`
	Directories []DirEntry     `yaml:"directories" json:"directories"`
}

// DotfileEntry represents a dotfile
type DotfileEntry struct {
	Source      string `yaml:"source" json:"source"`            // Path on source device
	Destination string `yaml:"destination" json:"destination"`  // Path on target device
	Checksum    string `yaml:"checksum" json:"checksum"`        // SHA256 for verification
	ContentHash string `yaml:"content_hash" json:"content_hash"` // Hash of actual content
}

// DirEntry represents a directory structure
type DirEntry struct {
	Source      string `yaml:"source" json:"source"`
	Destination string `yaml:"destination" json:"destination"`
	Recursive   bool   `yaml:"recursive" json:"recursive"`
}

// PreferencesGroup groups system and app preferences
type PreferencesGroup struct {
	Finder   FinderPrefs   `yaml:"finder" json:"finder"`
	Dock     DockPrefs     `yaml:"dock" json:"dock"`
	Keyboard KeyboardPrefs `yaml:"keyboard" json:"keyboard"`
	Trackpad TrackpadPrefs `yaml:"trackpad" json:"trackpad"`
	System   SystemPrefs   `yaml:"system" json:"system"`
	Apps     map[string]interface{} `yaml:"apps" json:"apps"` // App-specific preferences (plist)
}

// FinderPrefs for Finder settings
type FinderPrefs struct {
	ShowHiddenFiles bool   `yaml:"show_hidden_files" json:"show_hidden_files"`
	DefaultViewMode string `yaml:"default_view_mode" json:"default_view_mode"` // icon, list, column, cover
}

// DockPrefs for Dock settings
type DockPrefs struct {
	Position      string `yaml:"position" json:"position"`           // left, right, bottom
	Autohide      bool   `yaml:"autohide" json:"autohide"`
	ShowRecents   bool   `yaml:"show_recents" json:"show_recents"`
	AppOrder      []string `yaml:"app_order" json:"app_order"`       // Bundle IDs in order
	PersistentApps []string `yaml:"persistent_apps" json:"persistent_apps"`
}

// KeyboardPrefs for Keyboard settings
type KeyboardPrefs struct {
	KeyRepeat       int  `yaml:"key_repeat" json:"key_repeat"`
	InitialRepeat   int  `yaml:"initial_repeat" json:"initial_repeat"`
	NumLock         bool `yaml:"num_lock" json:"num_lock"`
}

// TrackpadPrefs for Trackpad settings
type TrackpadPrefs struct {
	Tracking        int  `yaml:"tracking" json:"tracking"`
	Clicking        bool `yaml:"clicking" json:"clicking"`
	ThreeFingerDrag bool `yaml:"three_finger_drag" json:"three_finger_drag"`
}

// SystemPrefs for general system settings
type SystemPrefs struct {
	ComputerName    string `yaml:"computer_name" json:"computer_name"`
	TimeZone        string `yaml:"time_zone" json:"time_zone"`
	Language        string `yaml:"language" json:"language"`
	ScreenBrightness int  `yaml:"screen_brightness" json:"screen_brightness"`
}

// EnvironmentGroup groups shell environment configuration
type EnvironmentGroup struct {
	Shell         string            `yaml:"shell" json:"shell"`
	ShellProfile  string            `yaml:"shell_profile" json:"shell_profile"` // ~/.zshrc, ~/.bash_profile
	EnvironmentVars map[string]string `yaml:"env_vars" json:"env_vars"`
	Aliases       map[string]string `yaml:"aliases" json:"aliases"`
	Functions     map[string]string `yaml:"functions" json:"functions"`
	Homebrew      HomebrewEnv       `yaml:"homebrew" json:"homebrew"`
	NodeVersion   string            `yaml:"node_version" json:"node_version"`
	PythonVersion string            `yaml:"python_version" json:"python_version"`
}

// HomebrewEnv captures Homebrew configuration
type HomebrewEnv struct {
	Prefix string `yaml:"prefix" json:"prefix"`
	Taps   []string `yaml:"taps" json:"taps"`
}

// MigrationTask represents a single migration action
type MigrationTask struct {
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name" json:"name"`
	Category    string `yaml:"category" json:"category"`     // apps, dotfiles, preferences, etc
	Action      string `yaml:"action" json:"action"`         // install, copy, configure
	Dry         bool   `yaml:"dry" json:"dry"`               // Dry-run mode
	Status      string `yaml:"status" json:"status"`         // pending, running, success, failed
	Error       string `yaml:"error" json:"error"`
	ExecutedAt  time.Time `yaml:"executed_at" json:"executed_at"`
}
