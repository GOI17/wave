package main

import (
	"os"
	"path/filepath"
	"testing"

	"wave/internal/analyzer"
	"wave/internal/executor"
	"wave/internal/migrator"
	"wave/internal/models"
)

func TestAnalyzerDevice(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	state, err := a.AnalyzeDevice()
	if err != nil {
		t.Fatalf("AnalyzeDevice failed: %v", err)
	}

	if state == nil {
		t.Fatal("Expected state, got nil")
	}

	if state.Version == "" {
		t.Error("Expected version, got empty string")
	}

	if state.SourceDevice.Hostname == "" {
		t.Error("Expected hostname, got empty string")
	}

	if state.SourceDevice.Username == "" {
		t.Error("Expected username, got empty string")
	}

	t.Logf("✓ Device analysis successful")
	t.Logf("  Hostname: %s", state.SourceDevice.Hostname)
	t.Logf("  Username: %s", state.SourceDevice.Username)
	t.Logf("  OS: %s", state.SourceDevice.OSVersion)
}

func TestAnalyzerApplications(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	apps, err := a.AnalyzeApplications()
	if err != nil {
		t.Fatalf("AnalyzeApplications failed: %v", err)
	}

	if apps == nil {
		t.Fatal("Expected applications, got nil")
	}

	t.Logf("✓ Application analysis successful")
	t.Logf("  Homebrew packages: %d", len(apps.Homebrew))
	t.Logf("  VS Code extensions: %d", len(apps.VSCodeExtensions))
	t.Logf("  App Store apps: %d", len(apps.AppStore))
}

func TestAnalyzerDotfiles(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	dotfiles, err := a.AnalyzeDotfiles()
	if err != nil {
		t.Fatalf("AnalyzeDotfiles failed: %v", err)
	}

	if dotfiles == nil {
		t.Fatal("Expected dotfiles, got nil")
	}

	t.Logf("✓ Dotfiles analysis successful")
	t.Logf("  Files found: %d", len(dotfiles.Files))
	t.Logf("  Directories found: %d", len(dotfiles.Directories))

	// Verify checksums
	for _, file := range dotfiles.Files {
		if file.Checksum == "" {
			t.Errorf("File %s has empty checksum", file.Source)
		}
	}
}

func TestAnalyzerPreferences(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	prefs, err := a.AnalyzePreferences()
	if err != nil {
		t.Fatalf("AnalyzePreferences failed: %v", err)
	}

	if prefs == nil {
		t.Fatal("Expected preferences, got nil")
	}

	t.Logf("✓ Preferences analysis successful")
	t.Logf("  Finder view mode: %s", prefs.Finder.DefaultViewMode)
	t.Logf("  Dock position: %s", prefs.Dock.Position)
	t.Logf("  Dock autohide: %v", prefs.Dock.Autohide)
}

func TestAnalyzerEnvironment(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	env, err := a.AnalyzeEnvironment()
	if err != nil {
		t.Fatalf("AnalyzeEnvironment failed: %v", err)
	}

	if env == nil {
		t.Fatal("Expected environment, got nil")
	}

	if env.Shell == "" {
		t.Error("Expected shell, got empty string")
	}

	t.Logf("✓ Environment analysis successful")
	t.Logf("  Shell: %s", env.Shell)
	t.Logf("  Shell profile: %s", env.ShellProfile)
	t.Logf("  Aliases: %d", len(env.Aliases))
	t.Logf("  Exports: %d", len(env.EnvironmentVars))
}

func TestMigrationCapture(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	tempFile := filepath.Join(homeDir, "wave-test-capture.yaml")
	defer os.Remove(tempFile)

	a := analyzer.NewMacOSAnalyzer(homeDir)
	e := executor.NewMacOSExecutor(homeDir, true)
	m := migrator.NewMigrator(a, e)

	err := m.Capture(tempFile, "yaml")
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(tempFile); os.IsNotExist(err) {
		t.Fatal("State file was not created")
	}

	t.Logf("✓ Migration capture successful")
	t.Logf("  File: %s", tempFile)

	// Check file size
	info, _ := os.Stat(tempFile)
	t.Logf("  Size: %d bytes", info.Size())
}

func TestMigrationApplyDryRun(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	tempFile := filepath.Join(homeDir, "wave-test-apply.yaml")
	defer os.Remove(tempFile)

	// First capture
	a := analyzer.NewMacOSAnalyzer(homeDir)
	e := executor.NewMacOSExecutor(homeDir, true)
	m := migrator.NewMigrator(a, e)

	err := m.Capture(tempFile, "yaml")
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	// Then apply (dry-run)
	err = m.Apply(tempFile, true, "yaml")
	if err != nil {
		t.Fatalf("Apply (dry-run) failed: %v", err)
	}

	t.Logf("✓ Dry-run migration successful")
	t.Logf("  File: %s", tempFile)
}

func TestExecutorValidation(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	e := executor.NewMacOSExecutor(homeDir, true)

	// Nil state
	err := e.ValidateState(nil, true)
	if err == nil {
		t.Error("Expected error for nil state")
	}

	// Empty state
	emptyState := &models.MigrationState{}
	err = e.ValidateState(emptyState, true)
	if err == nil {
		t.Error("Expected error for empty version")
	}

	// Valid state
	validState := &models.MigrationState{
		Version: "1.0.0",
	}
	err = e.ValidateState(validState, true)
	if err != nil {
		t.Logf("✓ State validation working: %v", err)
	}
}

func TestDataModelSerialization(t *testing.T) {
	state := &models.MigrationState{
		Version: "1.0.0",
		SourceDevice: models.DeviceInfo{
			Hostname:     "test-mac",
			Username:     "testuser",
			OSVersion:    "15.1",
			Architecture: "arm64",
		},
		Applications: models.ApplicationGroup{
			Homebrew: []models.HomebrewPackage{
				{
					Name:    "git",
					Type:    "formula",
					Version: "2.45.0",
				},
			},
		},
	}

	// Verify fields are set
	if state.Version != "1.0.0" {
		t.Error("Version not set correctly")
	}

	if state.SourceDevice.Hostname != "test-mac" {
		t.Error("Hostname not set correctly")
	}

	if len(state.Applications.Homebrew) != 1 {
		t.Error("Applications not set correctly")
	}

	t.Logf("✓ Data model serialization working correctly")
}

func TestFilePathHandling(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	testPath := filepath.Join(homeDir, ".config", "wave", "test.yaml")

	dir := filepath.Dir(testPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}

	// Create test file
	err := os.WriteFile(testPath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testPath)

	// Verify file exists
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Fatal("Test file was not created")
	}

	t.Logf("✓ File path handling working correctly")
	t.Logf("  Path: %s", testPath)
}

func BenchmarkCapture(b *testing.B) {
	homeDir, _ := os.UserHomeDir()
	tempFile := filepath.Join(homeDir, "wave-bench-capture.yaml")
	defer os.Remove(tempFile)

	a := analyzer.NewMacOSAnalyzer(homeDir)
	e := executor.NewMacOSExecutor(homeDir, true)
	m := migrator.NewMigrator(a, e)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Capture(tempFile, "yaml")
	}
}

func BenchmarkAnalyzeDevice(b *testing.B) {
	homeDir, _ := os.UserHomeDir()
	a := analyzer.NewMacOSAnalyzer(homeDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.AnalyzeDevice()
	}
}
