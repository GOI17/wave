package bundle_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
)

func TestCreateAndOpenPortableBundle(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".vimrc")
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
	if file.Destination != ".vimrc" {
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
	summary := bundle.FormatSummary(opened)
	if !strings.Contains(summary, "Portable Archive Contents") || !strings.Contains(summary, "- .vimrc (18 bytes, mode 0640)") {
		t.Fatalf("summary = %q", summary)
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

func TestCreateExcludesUnvettedRootDotfile(t *testing.T) {
	homeDir := t.TempDir()
	path := filepath.Join(homeDir, ".env")
	if err := os.WriteFile(path, []byte("THEME=darcula\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "device.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path}}}}
	if err := bundle.Create(bundlePath, homeDir, state); err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if len(opened.Manifest.Files) != 0 || opened.Manifest.Excluded != 1 {
		t.Fatalf("manifest = %#v, want unvetted file excluded", opened.Manifest)
	}
}

func TestCreateDeduplicatesIdenticalPayloads(t *testing.T) {
	homeDir := t.TempDir()
	var entries []models.DotfileEntry
	for _, name := range []string{".vimrc", ".editorconfig"} {
		path := filepath.Join(homeDir, name)
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

func TestCreateExcludesConfigDirectoriesAndCredentialContent(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".config", "safe-looking", "config.json")
	rootPath := filepath.Join(homeDir, ".zshrc")
	for _, path := range []string{configPath, rootPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(configPath, []byte(`{"theme":"darcula"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, []byte("export API_KEY=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: configPath}, {Source: rootPath}}}}
	path := filepath.Join(t.TempDir(), "device.wave")
	if err := bundle.Create(path, homeDir, state); err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if len(opened.Manifest.Files) != 0 || opened.Manifest.Excluded != 2 {
		t.Fatalf("manifest = %#v, want both files excluded", opened.Manifest)
	}
}

func TestCreateExcludesCommonCredentialAssignments(t *testing.T) {
	for i, content := range []string{
		"export GITHUB_TOKEN=ghp_secret\n",
		"AWS_SECRET_ACCESS_KEY=secret\n",
		"MY_SECRET=value\n",
		"SECRET=value\n",
		"readonly MY_TOKEN=value\n",
		`{"token":"value"}`,
		"url = https://user:password@example.com/repo.git\n",
		"url = https://ghp_token@github.com/repo.git\n",
		"url = //user:password@example.com/repo.git\n",
		"machine github.com login user password secret\n",
		"machine github.com\nlogin user\npassword secret\n",
		"GH_PAT=ghp_123456789012345678901234567890123456\n",
		"VALUE=gho_123456789012345678901234567890123456\n",
		"VALUE=github_pat_123456789012345678901234567890\n",
		"STRIPE_KEY=sk_" + "live_123456789012345678901234\n",
		"//registry.npmjs.org/:_authToken=secret\n",
	} {
		t.Run(fmt.Sprintf("credential-%d", i), func(t *testing.T) {
			homeDir := t.TempDir()
			path := filepath.Join(homeDir, ".zshrc")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			bundlePath := filepath.Join(t.TempDir(), "device.wave")
			state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path}}}}
			if err := bundle.Create(bundlePath, homeDir, state); err != nil {
				t.Fatal(err)
			}
			opened, err := bundle.Open(bundlePath)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			if len(opened.Manifest.Files) != 0 {
				t.Fatalf("credential content was bundled: %q", content)
			}
		})
	}
}

func TestCreateExcludesCredentialFilesByName(t *testing.T) {
	for name, content := range map[string]string{
		".netrc":  "machine github.com login user password secret\n",
		".pgpass": "localhost:5432:database:user:secret\n",
	} {
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			path := filepath.Join(homeDir, name)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			bundlePath := filepath.Join(t.TempDir(), "device.wave")
			state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path}}}}
			if err := bundle.Create(bundlePath, homeDir, state); err != nil {
				t.Fatal(err)
			}
			opened, err := bundle.Open(bundlePath)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			if len(opened.Manifest.Files) != 0 {
				t.Fatalf("%s was bundled", name)
			}
		})
	}
}

func TestCreateExcludesSymlinkToSensitiveOrNestedPath(t *testing.T) {
	homeDir := t.TempDir()
	for i, target := range []string{
		filepath.Join(homeDir, ".ssh", "config"),
		filepath.Join(homeDir, ".config", "tool", "config"),
	} {
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(homeDir, fmt.Sprintf(".safe-looking-%d", i))
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		bundlePath := filepath.Join(t.TempDir(), "device.wave")
		state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: link}}}}
		if err := bundle.Create(bundlePath, homeDir, state); err != nil {
			t.Fatal(err)
		}
		opened, err := bundle.Open(bundlePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(opened.Manifest.Files) != 0 {
			_ = opened.Close()
			t.Fatalf("symlink target was bundled: %s", target)
		}
		_ = opened.Close()
	}
}
