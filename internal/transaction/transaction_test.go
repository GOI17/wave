package transaction_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
	"wave/internal/transaction"
)

func TestApplyAndRollbackFiles(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	relative := filepath.Join(".config", "tool", "config.toml")
	sourcePath := filepath.Join(sourceHome, relative)
	targetPath := filepath.Join(targetHome, relative)
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("new\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
		Source: sourcePath, Destination: sourcePath,
	}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}

	system := newFakeSystem()
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if journal.ID == "" || journal.Status != "applied" {
		t.Fatalf("journal = %#v", journal)
	}
	assertFile(t, targetPath, "new\n", 0640)

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Restored != 1 || result.Conflicts != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
	assertFile(t, targetPath, "old\n", 0600)
}

func TestRollbackRefusesPostApplyChanges(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	relative := ".zshrc"
	sourcePath := filepath.Join(sourceHome, relative)
	targetPath := filepath.Join(targetHome, relative)
	if err := os.WriteFile(sourcePath, []byte("captured\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
		Source: sourcePath, Destination: sourcePath,
	}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("captured\nuser edit\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Conflicts != 1 || result.Restored != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
	assertFile(t, targetPath, "captured\nuser edit\n", 0600)
}

func TestRollbackRemovesFileCreatedByApply(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".new-config")
	if err := os.WriteFile(sourcePath, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
		Source: sourcePath, Destination: sourcePath,
	}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 {
		t.Fatalf("rollback result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(targetHome, ".new-config")); !os.IsNotExist(err) {
		t.Fatalf("created file still exists: %v", err)
	}
}

func TestApplyRejectsSymlinkedParentOutsideHome(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	outsideDir := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".config", "tool", "config")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(targetHome, ".config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(targetHome, ".config", "tool")); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}

	_, err := transaction.ApplyWithSystem(bundlePath, targetHome, filepath.Join(targetHome, ".wave", "transactions"), newFakeSystem())
	if err == nil {
		t.Fatal("Apply() accepted a destination through an external symlink")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "config")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}

func TestRollbackRefusesPostApplyModeChange(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".zshrc")
	targetPath := filepath.Join(targetHome, ".zshrc")
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	system := newFakeSystem()
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("rollback result = %#v, want mode conflict", result)
	}
	assertFile(t, targetPath, "captured", 0644)
}

func TestApplyAndRollbackPreferencesAndNewPackages(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{
		Version: "1.0.0",
		Applications: models.ApplicationGroup{
			Homebrew:         []models.HomebrewPackage{{Name: "jq", Type: "formula"}},
			VSCodeExtensions: []string{"publisher.extension"},
		},
		Preferences: models.PreferencesGroup{
			Finder: models.FinderPrefs{ShowHiddenFiles: true},
			Dock:   models.DockPrefs{Autohide: true},
		},
	}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	system.preferences["com.apple.finder:AppleShowAllFiles"] = fakePreference{kind: "bool", value: "false"}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")

	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("ApplyWithSystem() error = %v", err)
	}
	if system.preferences["com.apple.finder:AppleShowAllFiles"].value != "true" || system.packages["homebrew:jq"] == "" || !system.extensions["publisher.extension"] {
		t.Fatalf("system was not applied: %#v %#v %#v", system.preferences, system.packages, system.extensions)
	}

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("RollbackWithSystem() error = %v", err)
	}
	if result.PreferencesRestored == 0 || result.PackagesRemoved != 2 {
		t.Fatalf("rollback result = %#v", result)
	}
	if system.preferences["com.apple.finder:AppleShowAllFiles"].value != "false" || system.packages["homebrew:jq"] != "" || system.extensions["publisher.extension"] {
		t.Fatalf("system was not rolled back: %#v %#v %#v", system.preferences, system.packages, system.extensions)
	}
}

func TestRollbackRefusesChangedPreferenceAndPackage(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{
		Version:      "1.0.0",
		Applications: models.ApplicationGroup{Homebrew: []models.HomebrewPackage{{Name: "jq", Type: "formula"}}},
		Preferences:  models.PreferencesGroup{Finder: models.FinderPrefs{ShowHiddenFiles: true}},
	}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	system.preferences["com.apple.finder:AppleShowAllFiles"] = fakePreference{kind: "bool", value: "false"}
	system.packages["homebrew:jq"] = "jq 2.0"

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts < 2 {
		t.Fatalf("rollback result = %#v, want preference and package conflicts", result)
	}
}

type fakePreference struct {
	kind  string
	value string
}

type fakeSystem struct {
	preferences map[string]fakePreference
	packages    map[string]string
	extensions  map[string]bool
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		preferences: make(map[string]fakePreference),
		packages:    make(map[string]string),
		extensions:  make(map[string]bool),
	}
}

func (s *fakeSystem) Output(name string, args ...string) (string, error) {
	if name == "defaults" {
		key := args[1] + ":" + args[2]
		value, ok := s.preferences[key]
		if !ok {
			return "", fmt.Errorf("not found")
		}
		if args[0] == "read-type" {
			return map[string]string{"bool": "Type is boolean", "int": "Type is integer", "string": "Type is string"}[value.kind], nil
		}
		return value.value, nil
	}
	if name == "brew" {
		packageName := args[len(args)-1]
		version := s.packages["homebrew:"+packageName]
		if version == "" {
			return "", fmt.Errorf("not installed")
		}
		return version, nil
	}
	if name == "code" {
		var installed []string
		for extension, exists := range s.extensions {
			if exists {
				installed = append(installed, extension)
			}
		}
		return strings.Join(installed, "\n"), nil
	}
	return "", fmt.Errorf("unsupported command")
}

func (s *fakeSystem) Run(name string, args ...string) error {
	if name == "defaults" {
		key := args[1] + ":" + args[2]
		if args[0] == "delete" {
			delete(s.preferences, key)
			return nil
		}
		s.preferences[key] = fakePreference{kind: strings.TrimPrefix(args[3], "-"), value: args[4]}
		return nil
	}
	if name == "brew" {
		packageName := args[len(args)-1]
		key := "homebrew:" + packageName
		if args[0] == "install" {
			s.packages[key] = packageName + " 1.0"
		} else {
			delete(s.packages, key)
		}
		return nil
	}
	if name == "code" {
		extension := args[len(args)-1]
		s.extensions[extension] = args[0] == "--install-extension"
		return nil
	}
	return fmt.Errorf("unsupported command")
}

func assertFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("%s content = %q, want %q", path, data, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
	}
}
