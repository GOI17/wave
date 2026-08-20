package transaction_test

import (
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

func TestApplyLeavesPreferencesAndPackagesPreviewOnly(t *testing.T) {
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
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")

	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if result.Restored != 0 || result.Removed != 0 || result.Conflicts != 0 {
		t.Fatalf("rollback result = %#v", result)
	}
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
