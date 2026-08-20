package bundle_test

import (
	"os"
	"path/filepath"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
)

func TestCreateAndOpenPortableBundle(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".config", "tool", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("theme = 'darcula'\n"), 0640); err != nil {
		t.Fatal(err)
	}

	state := &models.MigrationState{
		Version: "1.0.0",
		Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
			Source:      configPath,
			Destination: configPath,
		}}},
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")

	if err := bundle.Create(bundlePath, homeDir, state); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	info, err := os.Stat(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("bundle mode = %o, want 600", info.Mode().Perm())
	}

	opened, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer opened.Close()

	if len(opened.Manifest.Files) != 1 {
		t.Fatalf("files = %#v, want one file", opened.Manifest.Files)
	}
	file := opened.Manifest.Files[0]
	if file.Destination != ".config/tool/config.toml" {
		t.Fatalf("destination = %q, want relative home path", file.Destination)
	}
	if file.Mode != 0640 {
		t.Fatalf("mode = %o, want 640", file.Mode)
	}
	data, err := opened.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "theme = 'darcula'\n" {
		t.Fatalf("data = %q", data)
	}
}

func TestCreateExcludesSensitiveAndUnsafeFiles(t *testing.T) {
	homeDir := t.TempDir()
	outsideDir := t.TempDir()
	paths := []string{
		filepath.Join(homeDir, ".ssh", "github_personal"),
		filepath.Join(homeDir, ".config", "gcloud", "credentials.db"),
		filepath.Join(homeDir, ".config", "tool", "api-token"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	externalPath := filepath.Join(outsideDir, "external")
	if err := os.WriteFile(externalPath, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(homeDir, ".config", "external-link")
	if err := os.Symlink(externalPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, symlinkPath)

	entries := make([]models.DotfileEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, models.DotfileEntry{Source: path, Destination: path})
	}
	state := &models.MigrationState{
		Version:     "1.0.0",
		Dotfiles:    models.DotfilesGroup{Files: entries},
		Environment: models.EnvironmentGroup{EnvironmentVars: map[string]string{"API_TOKEN": "secret-value"}},
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")

	if err := bundle.Create(bundlePath, homeDir, state); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	if len(opened.Manifest.Files) != 0 {
		t.Fatalf("sensitive files were bundled: %#v", opened.Manifest.Files)
	}
	if opened.Manifest.Excluded != len(paths) {
		t.Fatalf("excluded = %d, want %d", opened.Manifest.Excluded, len(paths))
	}
	if len(opened.Manifest.State.Dotfiles.Files) != 0 || len(opened.Manifest.State.Environment.EnvironmentVars) != 0 {
		t.Fatalf("portable state retained sensitive path/environment metadata: %#v", opened.Manifest.State)
	}
}

func TestCreateDeduplicatesIdenticalPayloads(t *testing.T) {
	homeDir := t.TempDir()
	var entries []models.DotfileEntry
	for _, name := range []string{"one", "two"} {
		path := filepath.Join(homeDir, ".config", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, models.DotfileEntry{Source: path})
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	if err := bundle.Create(bundlePath, homeDir, &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: entries}}); err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer opened.Close()
	if len(opened.Manifest.Files) != 2 || opened.Manifest.Files[0].Payload != opened.Manifest.Files[1].Payload {
		t.Fatalf("files = %#v, want shared payload", opened.Manifest.Files)
	}
}
