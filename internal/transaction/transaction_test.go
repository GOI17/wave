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
	relative := ".vimrc"
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

	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if journal.ID == "" || journal.Status != "applied" {
		t.Fatalf("journal = %#v", journal)
	}
	assertFile(t, targetPath, "new\n", 0640)

	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Restored != 1 || result.Conflicts != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
	assertFile(t, targetPath, "old\n", 0600)
}

func TestRollbackRestoresOriginalInodeAndHardLinks(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".vimrc")
	targetPath := filepath.Join(targetHome, ".vimrc")
	hardLink := filepath.Join(targetHome, ".vimrc-link")
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(targetPath, hardLink); err != nil {
		t.Fatal(err)
	}
	originalInfo, _ := os.Stat(targetPath)
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Rollback(journal.ID, targetHome, transactionsDir); err != nil {
		t.Fatal(err)
	}
	restoredInfo, _ := os.Stat(targetPath)
	linkInfo, _ := os.Stat(hardLink)
	if !os.SameFile(originalInfo, restoredInfo) || !os.SameFile(restoredInfo, linkInfo) {
		t.Fatal("rollback did not restore original inode and hard-link identity")
	}
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
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("captured\nuser edit\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Conflicts != 1 || result.Restored != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
	assertFile(t, targetPath, "captured\nuser edit\n", 0600)
}

func TestRollbackRefusesInPlaceChangesToNewFile(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".editorconfig")
	targetPath := filepath.Join(targetHome, ".editorconfig")
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("user edit"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 || result.Removed != 0 {
		t.Fatalf("rollback result = %#v, want conflict", result)
	}
	assertFile(t, targetPath, "user edit", 0600)
}

func TestRollbackRemovesFileCreatedByApply(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".prettierrc")
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
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	appliedInfo, err := os.Stat(filepath.Join(targetHome, ".prettierrc"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 {
		t.Fatalf("rollback result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(targetHome, ".prettierrc")); !os.IsNotExist(err) {
		t.Fatalf("created file still exists: %v", err)
	}
	preservedInfo, err := os.Stat(filepath.Join(transactionsDir, journal.ID, journal.Files[0].Staged))
	if err != nil || !os.SameFile(appliedInfo, preservedInfo) {
		t.Fatalf("applied inode was not retained: %v", err)
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
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 {
		t.Fatalf("rollback result = %#v, want mode conflict", result)
	}
	assertFile(t, targetPath, "captured", 0644)
}

func TestApplyRejectsDestinationSymlink(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".zshrc")
	targetPath := filepath.Join(targetHome, ".zshrc")
	realTarget := filepath.Join(targetHome, "real-zshrc")
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realTarget, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTarget, targetPath); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	_, err := transaction.Apply(bundlePath, targetHome, filepath.Join(targetHome, ".wave", "transactions"))
	if err == nil || !strings.Contains(err.Error(), "symlinks are not supported") {
		t.Fatalf("error = %v, want destination symlink rejection", err)
	}
	info, err := os.Lstat(targetPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("destination symlink was replaced: %v, %#v", err, info)
	}
	assertFile(t, realTarget, "original", 0600)
}

func TestApplyAndRollbackApplicationsAndPreferences(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{
		Version: "1.0.0",
		Applications: models.ApplicationGroup{
			Homebrew:         []models.HomebrewPackage{{Name: "jq", Type: "formula"}},
			AppStore:         []models.AppStoreApp{{BundleID: "12345", Name: "Example"}},
			Manual:           []models.ManualApp{{Name: "Manual Tool", Path: "/Applications/Manual Tool.app"}},
			VSCodeExtensions: []string{"publisher.extension"},
		},
		Preferences: models.PreferencesGroup{
			Finder:   models.FinderPrefs{ShowHiddenFiles: true, DefaultViewMode: "clmv"},
			Dock:     models.DockPrefs{Autohide: true, ShowRecents: false, Position: "left"},
			Keyboard: models.KeyboardPrefs{KeyRepeat: 2, InitialRepeat: 15},
			System:   models.SystemPrefs{ComputerName: "Migrated Mac", TimeZone: "America/Chicago", Language: "en"},
		},
	}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")

	system := newFakeSystem()
	system.preferences["com.apple.finder:AppleShowAllFiles"] = fakePreference{kind: "bool", value: "false"}
	system.preferences["com.apple.dock:autohide"] = fakePreference{kind: "bool", value: "false"}
	system.preferences["com.apple.dock:show-recents"] = fakePreference{kind: "bool", value: "true"}
	system.system["computer-name"] = "Original Mac"
	system.system["time-zone"] = "UTC"
	system.system["language"] = "es"

	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(journal.Packages) != 3 || len(journal.Preferences) != 7 || len(journal.System) != 3 || len(journal.Unresolved) != 1 {
		t.Fatalf("journal = %#v", journal)
	}

	result, err := transaction.RollbackWithSystem(journal.ID, targetHome, transactionsDir, system)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.PreferencesRestored != 7 || result.PackagesRemoved != 0 || result.ApplicationsRetained != 3 || result.SystemRestored != 3 || result.Conflicts != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
	if _, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, system); err != nil {
		t.Fatalf("Apply() after retained-app rollback error = %v", err)
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
	appStore    map[string]bool
	system      map[string]string
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{preferences: map[string]fakePreference{}, packages: map[string]string{}, extensions: map[string]bool{}, appStore: map[string]bool{}, system: map[string]string{}}
}

func (s *fakeSystem) Output(name string, args ...string) (string, error) {
	switch name {
	case "defaults":
		if args[0] == "read" && args[1] == "-g" && args[2] == "AppleLanguages" {
			return s.system["language"], nil
		}
		key := args[1] + ":" + args[2]
		value, ok := s.preferences[key]
		if !ok {
			return "", fmt.Errorf("not found")
		}
		if args[0] == "read-type" {
			return map[string]string{"bool": "Type is boolean", "int": "Type is integer", "string": "Type is string"}[value.kind], nil
		}
		return value.value, nil
	case "brew":
		var installed []string
		for name := range s.packages {
			installed = append(installed, name)
		}
		return strings.Join(installed, "\n"), nil
	case "code":
		var installed []string
		for extension, exists := range s.extensions {
			if exists {
				installed = append(installed, extension)
			}
		}
		return strings.Join(installed, "\n"), nil
	case "mas":
		var installed []string
		for id, exists := range s.appStore {
			if exists {
				installed = append(installed, id+" Example")
			}
		}
		return strings.Join(installed, "\n"), nil
	case "scutil":
		return s.system["computer-name"], nil
	case "systemsetup":
		return "", fmt.Errorf("unsupported direct systemsetup read")
	case "readlink":
		return "/usr/share/zoneinfo/" + s.system["time-zone"], nil
	default:
		return "", fmt.Errorf("unsupported output: %s", name)
	}
}

func (s *fakeSystem) Run(name string, args ...string) error {
	switch name {
	case "defaults":
		if args[0] == "delete" {
			delete(s.preferences, args[1]+":"+args[2])
			return nil
		}
		if args[1] == "-g" && args[2] == "AppleLanguages" {
			s.system["language"] = strings.Join(args[4:], ",")
			return nil
		}
		s.preferences[args[1]+":"+args[2]] = fakePreference{kind: strings.TrimPrefix(args[3], "-"), value: args[4]}
	case "brew":
		name := args[len(args)-1]
		if args[0] == "install" {
			s.packages[name] = name + " 1.0"
		} else {
			delete(s.packages, name)
		}
	case "code":
		s.extensions[args[1]] = args[0] == "--install-extension"
	case "mas":
		s.appStore[args[1]] = args[0] == "install"
	case "osascript":
		script := args[1]
		if strings.Contains(script, "scutil") {
			if strings.Contains(script, "Original Mac") {
				s.system["computer-name"] = "Original Mac"
			} else {
				s.system["computer-name"] = "Migrated Mac"
			}
		} else if strings.Contains(script, "systemsetup") {
			if strings.Contains(script, "America/Chicago") {
				s.system["time-zone"] = "America/Chicago"
			} else {
				s.system["time-zone"] = "UTC"
			}
		}
	default:
		return fmt.Errorf("unsupported run: %s", name)
	}
	return nil
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
