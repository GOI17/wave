package share

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
)

func TestNativeShareScriptCompiles(t *testing.T) {
	command := exec.Command("/usr/bin/swiftc", "-typecheck", "-")
	command.Stdin = strings.NewReader(script)
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native share script does not compile: %v: %s", err, combined)
	}
}

func TestArchiveValidatesAndSharesWaveBundle(t *testing.T) {
	homeDir := t.TempDir()
	path := filepath.Join(homeDir, "state.wave")
	if err := bundle.Create(path, homeDir, &models.MigrationState{Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	original := run
	defer func() { run = original }()
	var shared string
	run = func(path string) error {
		shared = path
		return nil
	}
	if err := Archive(path); err != nil {
		t.Fatal(err)
	}
	if shared != path {
		t.Fatalf("shared = %q, want %q", shared, path)
	}
}

func TestArchiveRejectsInvalidBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.wave")
	if err := os.WriteFile(path, []byte("not a bundle"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Archive(path); err == nil {
		t.Fatal("Archive() accepted invalid bundle")
	}
}
