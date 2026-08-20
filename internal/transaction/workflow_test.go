package transaction_test

import (
	"os"
	"path/filepath"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
	"wave/internal/transaction"
)

func TestPreviewAndLatestTransaction(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".config", "tool")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}

	preview, err := transaction.Preview(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || preview.Successful == 0 {
		t.Fatalf("preview = %#v", preview)
	}
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	journal, err := transaction.ApplyWithSystem(bundlePath, targetHome, transactionsDir, newFakeSystem())
	if err != nil {
		t.Fatal(err)
	}
	latest, err := transaction.Latest(transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != journal.ID {
		t.Fatalf("latest = %q, want %q", latest.ID, journal.ID)
	}
}
