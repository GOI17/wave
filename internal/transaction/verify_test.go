package transaction

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
)

type readOnlyVerificationSystem struct{ t *testing.T }

func (s readOnlyVerificationSystem) Run(string, ...string) error {
	s.t.Fatal("verification attempted a mutation")
	return nil
}
func (s readOnlyVerificationSystem) Output(string, ...string) (string, error) {
	return "", fmt.Errorf("unavailable")
}

func TestVerifyReadsActualBytesPermissionsAndFileType(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	path := filepath.Join(source, ".vimrc")
	if err := os.WriteFile(path, []byte("set number\n"), 0640); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "source.wave")
	state := &models.MigrationState{Version: "1", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path, Destination: path}}}}
	if err := bundle.Create(archive, source, state); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(target, ".vimrc")
	check := func(want bool) {
		t.Helper()
		result, err := VerifyWithSystem(archive, target, readOnlyVerificationSystem{t})
		if err != nil || result.Complete() != want {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		if _, err := os.Stat(filepath.Join(target, ".wave")); !os.IsNotExist(err) {
			t.Fatal("verification wrote transaction data")
		}
	}
	check(false)
	if err := os.WriteFile(destination, []byte("set number\n"), 0640); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.Chmod(destination, 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Chmod(destination, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("set nonumber\n"), 0640); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, destination); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestVerifyExcludedSourceFilesPreventCompleteResult(t *testing.T) {
	source := t.TempDir()
	path := filepath.Join(source, "document.txt")
	if err := os.WriteFile(path, []byte("personal document"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "source.wave")
	state := &models.MigrationState{Version: "1", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path, Destination: path}}}}
	if err := bundle.Create(archive, source, state); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyWithSystem(archive, t.TempDir(), readOnlyVerificationSystem{t})
	if err != nil || result.Complete() || len(result.Unverified) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestVerifyMissingPreferencesAreDifferences(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "source.wave")
	state := &models.MigrationState{Version: "1", Preferences: models.PreferencesGroup{Dock: models.DockPrefs{Position: "left"}}}
	if err := bundle.Create(archive, t.TempDir(), state); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyWithSystem(archive, t.TempDir(), readOnlyVerificationSystem{t})
	if err != nil || result.Complete() || len(result.Differences) == 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestVerifyHomebrewRequiresCapturedVersions(t *testing.T) {
	expected := []models.HomebrewPackage{{Name: "git", Type: "formula", Version: "git 2.55.0"}, {Name: "app", Type: "cask", Version: "1.0"}}
	actual := []models.HomebrewPackage{{Name: "git", Type: "formula", Version: "2.55.0"}, {Name: "app", Type: "cask", Version: "2.0"}}
	result := &Verification{}
	compareHomebrew(result, expected, actual)
	if result.Matched != 1 || len(result.Differences) != 1 || result.Complete() {
		t.Fatalf("result=%+v", result)
	}
	expected[0].Version = ""
	result = &Verification{}
	compareHomebrew(result, expected[:1], actual)
	if len(result.Unverified) != 1 || result.Complete() {
		t.Fatalf("result=%+v", result)
	}
}
