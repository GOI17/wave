package transaction_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
	"wave/internal/transaction"
)

func TestPreviewAndLatestTransaction(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	sourcePath := filepath.Join(sourceHome, ".vimrc")
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
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
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

func TestApplyRecoversUnfinishedTransactionFirst(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".vimrc")
	targetPath := filepath.Join(targetHome, ".vimrc")
	_ = os.WriteFile(sourcePath, []byte("captured"), 0600)
	_ = os.WriteFile(targetPath, []byte("original"), 0600)
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	journal.Status = "preparing"
	data, _ := json.Marshal(journal)
	if err := os.WriteFile(filepath.Join(transactionsDir, journal.ID, "journal.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Apply(bundlePath, targetHome, transactionsDir); err != nil {
		t.Fatalf("second apply did not recover unfinished transaction: %v", err)
	}
}

func TestApplyBlocksOnUnresolvedRollbackConflicts(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".vimrc")
	targetPath := filepath.Join(targetHome, ".vimrc")
	_ = os.WriteFile(sourcePath, []byte("captured"), 0600)
	_ = os.WriteFile(targetPath, []byte("original"), 0600)
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("user edit"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := transaction.Rollback(journal.ID, targetHome, transactionsDir)
	if err != nil || result.Conflicts != 1 {
		t.Fatalf("Rollback() = %#v, %v", result, err)
	}
	journalPath := filepath.Join(transactionsDir, journal.ID, "journal.json")
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted transaction.Journal
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != "rollback-conflicts" {
		t.Fatalf("persisted status = %q, want rollback-conflicts", persisted.Status)
	}
	if _, err := transaction.Apply(bundlePath, targetHome, transactionsDir); err == nil || !strings.Contains(err.Error(), "requires rollback resolution") {
		t.Fatalf("Apply() error = %v, want unresolved conflict rejection", err)
	}
}

func TestApplyRecoversCrashBeforeExistingFileSwap(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".vimrc")
	targetPath := filepath.Join(targetHome, ".vimrc")
	_ = os.WriteFile(sourcePath, []byte("captured"), 0600)
	_ = os.WriteFile(targetPath, []byte("original"), 0600)
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Rollback(journal.ID, targetHome, transactionsDir); err != nil {
		t.Fatal(err)
	}
	journal.Status = "preparing"
	journal.Files[0].Applying = true
	journal.Files[0].Applied = false
	journal.Files[0].RolledBack = false
	journal.Files[0].PreservedApplied = ""
	data, _ := json.Marshal(journal)
	if err := os.WriteFile(filepath.Join(transactionsDir, journal.ID, "journal.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Apply(bundlePath, targetHome, transactionsDir); err != nil {
		t.Fatalf("Apply() did not recover pre-swap crash: %v", err)
	}
}

func TestApplyRecoversCrashAfterNewFileLink(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	sourcePath := filepath.Join(sourceHome, ".editorconfig")
	_ = os.WriteFile(sourcePath, []byte("captured"), 0600)
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	journal, err := transaction.Apply(bundlePath, targetHome, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	journal.Status = "preparing"
	journal.Files[0].Applying = true
	journal.Files[0].Applied = false
	data, _ := json.Marshal(journal)
	if err := os.WriteFile(filepath.Join(transactionsDir, journal.ID, "journal.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Apply(bundlePath, targetHome, transactionsDir); err != nil {
		t.Fatalf("Apply() did not recover post-link crash: %v", err)
	}
}

func TestRollbackRejectsLegacyJournalSchema(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	id := "legacy"
	dir := filepath.Join(transactionsDir, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"legacy","status":"applied","packages":[{"name":"jq"}]}`
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Rollback(id, homeDir, transactionsDir); err == nil {
		t.Fatal("legacy journal was accepted")
	}
}

func TestLatestMigratesKnownUnversionedFileOnlyJournal(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	id := "20260101T000000Z-abcdef"
	dir := filepath.Join(transactionsDir, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := fmt.Sprintf(`{"id":%q,"created_at":"2026-01-01T00:00:00Z","status":"applied","files":[]}`, id)
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	latest, err := transaction.Latest(transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != 1 || latest.ID != id {
		t.Fatalf("latest = %#v, want migrated v1 journal", latest)
	}
}

func TestApplyFailsClosedOnMalformedExistingTransaction(t *testing.T) {
	sourceHome := t.TempDir()
	targetHome := t.TempDir()
	transactionsDir := filepath.Join(targetHome, ".wave", "transactions")
	badDir := filepath.Join(transactionsDir, "corrupt")
	if err := os.MkdirAll(badDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceHome, ".vimrc")
	if err := os.WriteFile(sourcePath, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}}}
	if err := bundle.Create(bundlePath, sourceHome, state); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Apply(bundlePath, targetHome, transactionsDir); err == nil {
		t.Fatal("Apply() ignored a malformed existing transaction")
	}
	if _, err := os.Stat(filepath.Join(targetHome, ".vimrc")); !os.IsNotExist(err) {
		t.Fatalf("target was mutated despite malformed transaction: %v", err)
	}
}

func TestApplyRejectsSemanticallyInvalidJournal(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	dir := filepath.Join(transactionsDir, "invalid")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	invalid := `{"version":1,"status":"applied"}`
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Latest(transactionsDir); err == nil {
		t.Fatal("semantically invalid journal was accepted")
	}
}

func TestQuarantineUnblocksMalformedTransaction(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	id := "invalid"
	dir := filepath.Join(transactionsDir, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	destination, err := transaction.Quarantine(id, transactionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("quarantined transaction is missing: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("transaction was not moved: %v", err)
	}
}

func TestQuarantineRejectsCurrentDirectory(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	if _, err := transaction.Quarantine(".", transactionsDir); err == nil {
		t.Fatal("Quarantine() accepted current-directory transaction id")
	}
}

func TestQuarantineRejectsRootedTransactionID(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	if _, err := transaction.Quarantine("/", transactionsDir); err == nil {
		t.Fatal("Quarantine() accepted rooted transaction id")
	}
}

func TestQuarantineRejectsLockFile(t *testing.T) {
	homeDir := t.TempDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	if err := os.MkdirAll(transactionsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactionsDir, ".lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Quarantine(".lock", transactionsDir); err == nil {
		t.Fatal("Quarantine() accepted lock file")
	}
}

func TestCanonicalApplyAndRollbackSummaries(t *testing.T) {
	journal := &transaction.Journal{ID: "tx-123", Files: make([]transaction.FileJournal, 2)}
	applySummary := transaction.FormatApplySummary(journal)
	if !strings.Contains(applySummary, "Transaction: tx-123") || !strings.Contains(applySummary, "Root dotfiles applied: 2") || !strings.Contains(applySummary, "Applications: preview-only") {
		t.Fatalf("apply summary = %q", applySummary)
	}
	rollbackSummary := transaction.FormatRollbackSummary(&transaction.RollbackResult{TransactionID: "tx-123", Restored: 1, Conflicts: 1, ConflictPaths: []string{".zshrc"}})
	if !strings.Contains(rollbackSummary, "Conflicts preserved: 1") || !strings.Contains(rollbackSummary, "- .zshrc") {
		t.Fatalf("rollback summary = %q", rollbackSummary)
	}
}
