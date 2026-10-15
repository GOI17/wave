package transaction_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wave/internal/analyzer"
	"wave/internal/bundle"
	"wave/internal/models"
	"wave/internal/transaction"
)

// Exercise the real archive and transaction code between two synthetic machines.
// Only external application managers are simulated; file writes are real.
func TestSandboxMigrationRoundTrip(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(source, ".vimrc"), "set number\n", 0640)
	writeFixture(t, filepath.Join(source, ".editorconfig"), "root = true\n", 0600)
	writeFixture(t, filepath.Join(target, ".vimrc"), "set nonumber\n", 0600)
	dotfiles, err := analyzer.NewMacOSAnalyzer(source).AnalyzeDotfiles()
	if err != nil {
		t.Fatal(err)
	}
	state := &models.MigrationState{
		Version: "1.0.0", Dotfiles: *dotfiles,
		Applications: models.ApplicationGroup{
			Homebrew: []models.HomebrewPackage{
				{Name: "jq", Type: "formula"},
				{Name: "example", Type: "cask"},
			},
			AppStore: []models.AppStoreApp{{BundleID: "12345", Name: "Fixture App"}},
		},
	}
	archive := filepath.Join(t.TempDir(), "machine.wave")
	if err := bundle.Create(archive, source, state); err != nil {
		t.Fatal(err)
	}
	transactions := filepath.Join(target, ".wave", "transactions")
	system := newFakeSystem()
	// A formula and cask can share a name. A formula must not suppress a cask.
	system.packages["formula:example"] = "example 1.0"
	preview, err := transaction.Preview(archive)
	if err != nil || !preview.DryRun {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	assertFile(t, filepath.Join(target, ".vimrc"), "set nonumber\n", 0600)
	if _, err := os.Stat(transactions); !os.IsNotExist(err) {
		t.Fatalf("preview created transactions: %v", err)
	}
	journal, err := transaction.ApplyWithSystem(archive, target, transactions, system)
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(target, ".vimrc"), "set number\n", 0640)
	assertFile(t, filepath.Join(target, ".editorconfig"), "root = true\n", 0600)
	assertFile(t, filepath.Join(source, ".vimrc"), "set number\n", 0640)
	wantCalls := []string{"brew install jq", "brew install --cask example", "mas install 12345"}
	if !reflect.DeepEqual(system.calls, wantCalls) {
		t.Fatalf("install commands = %v, want %v", system.calls, wantCalls)
	}
	result, err := transaction.RollbackWithSystem(journal.ID, target, transactions, system)
	if err != nil || result.Conflicts != 0 || result.ApplicationsRetained != 3 {
		t.Fatalf("rollback = %#v, %v", result, err)
	}
	assertFile(t, filepath.Join(target, ".vimrc"), "set nonumber\n", 0600)
	if _, err := os.Stat(filepath.Join(target, ".editorconfig")); !os.IsNotExist(err) {
		t.Fatalf("rollback did not remove created file: %v", err)
	}
	if !reflect.DeepEqual(system.calls, wantCalls) {
		t.Fatalf("rollback ran application mutations: %v", system.calls)
	}
	// Re-applying must skip apps still installed after rollback.
	if _, err := transaction.ApplyWithSystem(archive, target, transactions, system); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(system.calls, wantCalls) {
		t.Fatalf("already installed apps were installed again: %v", system.calls)
	}
}

func TestSandboxFailedInstallCanRollBackFiles(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	sourcePath := filepath.Join(source, ".vimrc")
	writeFixture(t, sourcePath, "set number\n", 0640)
	writeFixture(t, filepath.Join(target, ".vimrc"), "original\n", 0600)
	archive := filepath.Join(t.TempDir(), "machine.wave")
	state := &models.MigrationState{
		Version:  "1.0.0",
		Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: sourcePath}}},
		Applications: models.ApplicationGroup{Homebrew: []models.HomebrewPackage{
			{Name: "jq", Type: "formula"}, {Name: "example", Type: "cask"},
		}},
	}
	if err := bundle.Create(archive, source, state); err != nil {
		t.Fatal(err)
	}
	system := newFakeSystem()
	system.failCommand = "brew install --cask example"
	transactions := filepath.Join(target, ".wave", "transactions")
	journal, err := transaction.ApplyWithSystem(archive, target, transactions, system)
	if err == nil || !strings.Contains(err.Error(), "simulated installation failure") || journal == nil {
		t.Fatalf("expected recoverable installation failure, got %#v, %v", journal, err)
	}
	if journal.Status != "rolled-back" {
		t.Fatalf("automatic rollback status = %q", journal.Status)
	}
	assertFile(t, filepath.Join(target, ".vimrc"), "original\n", 0600)
	if _, ok := system.packages["formula:jq"]; !ok {
		t.Fatal("rollback removed an installed application")
	}
	// The recovered journal must remain readable so that a retry is not blocked.
	system.failCommand = ""
	if _, err := transaction.ApplyWithSystem(archive, target, transactions, system); err != nil {
		t.Fatalf("retry after failed installation: %v", err)
	}
}

func writeFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
