package transaction_test

import (
	"os"
	"path/filepath"
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
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
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

	_, err := transaction.Apply(bundlePath, targetHome, filepath.Join(targetHome, ".wave", "transactions"))
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
