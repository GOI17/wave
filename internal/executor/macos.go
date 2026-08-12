package executor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wave/internal/models"
)

// macOSExecutor implements Executor for macOS
type macOSExecutor struct {
	homeDir string
	dryRun  bool
}

// NewMacOSExecutor creates a new macOS executor
func NewMacOSExecutor(homeDir string, dryRun bool) Executor {
	return &macOSExecutor{
		homeDir: homeDir,
		dryRun:  dryRun,
	}
}

// ExecuteApplications installs applications
func (e *macOSExecutor) ExecuteApplications(apps *models.ApplicationGroup, dryRun bool) ([]models.MigrationTask, error) {
	var tasks []models.MigrationTask
	e.dryRun = dryRun

	// Install Homebrew packages
	for _, pkg := range apps.Homebrew {
		task := models.MigrationTask{
			ID:       fmt.Sprintf("app-%s-%s", pkg.Type, pkg.Name),
			Name:     fmt.Sprintf("Install %s (%s)", pkg.Name, pkg.Type),
			Category: "applications",
			Action:   "install",
			Status:   "pending",
			Dry:      dryRun,
		}

		if err := e.installHomebrewPackage(pkg, dryRun); err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "success"
		}

		task.ExecutedAt = time.Now()
		tasks = append(tasks, task)
	}

	// Install VS Code extensions
	for _, ext := range apps.VSCodeExtensions {
		task := models.MigrationTask{
			ID:       fmt.Sprintf("vscode-%s", ext),
			Name:     fmt.Sprintf("Install VS Code extension: %s", ext),
			Category: "applications",
			Action:   "install",
			Status:   "pending",
			Dry:      dryRun,
		}

		if err := e.installVSCodeExtension(ext, dryRun); err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "success"
		}

		task.ExecutedAt = time.Now()
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// ExecuteDotfiles copies dotfiles to target
func (e *macOSExecutor) ExecuteDotfiles(dotfiles *models.DotfilesGroup, dryRun bool) ([]models.MigrationTask, error) {
	var tasks []models.MigrationTask
	e.dryRun = dryRun

	for _, dotfile := range dotfiles.Files {
		task := models.MigrationTask{
			ID:       fmt.Sprintf("dotfile-%s", strings.ReplaceAll(dotfile.Source, "/", "-")),
			Name:     fmt.Sprintf("Copy %s", filepath.Base(dotfile.Source)),
			Category: "dotfiles",
			Action:   "copy",
			Status:   "pending",
			Dry:      dryRun,
		}

		if err := e.copyDotfile(dotfile, dryRun); err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "success"
		}

		task.ExecutedAt = time.Now()
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// ExecutePreferences applies system preferences
func (e *macOSExecutor) ExecutePreferences(prefs *models.PreferencesGroup, dryRun bool) ([]models.MigrationTask, error) {
	var tasks []models.MigrationTask
	e.dryRun = dryRun

	// Apply Finder preferences
	if prefs.Finder.ShowHiddenFiles {
		task := e.applyDefault("com.apple.finder", "AppleShowAllFiles", "1", "Show hidden files", dryRun)
		tasks = append(tasks, task)
	}

	if prefs.Finder.DefaultViewMode != "" {
		task := e.applyDefault("com.apple.finder", "FXPreferredViewStyle", prefs.Finder.DefaultViewMode, "Set Finder view mode", dryRun)
		tasks = append(tasks, task)
	}

	// Apply Dock preferences
	if prefs.Dock.Position != "" {
		task := e.applyDefault("com.apple.dock", "orientation", prefs.Dock.Position, "Set Dock position", dryRun)
		tasks = append(tasks, task)
	}

	if prefs.Dock.Autohide {
		task := e.applyDefault("com.apple.dock", "autohide", "1", "Enable Dock autohide", dryRun)
		tasks = append(tasks, task)
	}

	// Apply Keyboard preferences
	if prefs.Keyboard.KeyRepeat > 0 {
		task := e.applyDefault("-g", "KeyRepeat", fmt.Sprintf("%d", prefs.Keyboard.KeyRepeat), "Set key repeat rate", dryRun)
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// ExecuteEnvironment configures shell environment
func (e *macOSExecutor) ExecuteEnvironment(env *models.EnvironmentGroup, dryRun bool) ([]models.MigrationTask, error) {
	var tasks []models.MigrationTask
	e.dryRun = dryRun

	shellProfile := env.ShellProfile
	if shellProfile == "" {
		shellProfile = filepath.Join(e.homeDir, ".zshrc")
	}

	// Backup original
	backupPath := shellProfile + ".backup"
	if _, err := os.Stat(shellProfile); err == nil {
		if !dryRun {
			os.Link(shellProfile, backupPath)
		}
	}

	// Read existing content
	var content string
	if data, err := os.ReadFile(shellProfile); err == nil {
		content = string(data)
	}

	// Add aliases
	aliasSection := "\n# Wave-managed aliases\n"
	for key, value := range env.Aliases {
		aliasSection += fmt.Sprintf("alias %s='%s'\n", key, value)
	}

	// Add exports
	exportSection := "\n# Wave-managed exports\n"
	for key, value := range env.EnvironmentVars {
		if !strings.Contains(key, "PATH") { // Skip PATH for now
			exportSection += fmt.Sprintf("export %s=%s\n", key, value)
		}
	}

	// Write back
	if !dryRun && (len(env.Aliases) > 0 || len(env.EnvironmentVars) > 0) {
		newContent := content + aliasSection + exportSection
		os.WriteFile(shellProfile, []byte(newContent), 0644)
	}

	task := models.MigrationTask{
		ID:         "env-config",
		Name:       "Configure shell environment",
		Category:   "environment",
		Action:     "configure",
		Status:     "success",
		Dry:        dryRun,
		ExecutedAt: time.Now(),
	}
	tasks = append(tasks, task)

	return tasks, nil
}

// ValidateState checks target device compatibility
func (e *macOSExecutor) ValidateState(state *models.MigrationState) error {
	if state == nil {
		return fmt.Errorf("migration state is nil")
	}
	if state.Version == "" {
		return fmt.Errorf("migration state version is empty")
	}

	// Check if on macOS
	if _, err := exec.Command("uname", "-s").Output(); err != nil {
		return fmt.Errorf("not running on macOS")
	}

	return nil
}

// Helper methods

func (e *macOSExecutor) installHomebrewPackage(pkg models.HomebrewPackage, dryRun bool) error {
	var cmd *exec.Cmd

	if pkg.Type == "cask" {
		cmd = exec.Command("brew", "install", "--cask", pkg.Name)
	} else {
		cmd = exec.Command("brew", "install", pkg.Name)
	}

	if dryRun {
		fmt.Printf("DRY-RUN: Would execute: %v\n", cmd.Args)
		return nil
	}

	return cmd.Run()
}

func (e *macOSExecutor) installVSCodeExtension(ext string, dryRun bool) error {
	if dryRun {
		fmt.Printf("DRY-RUN: Would install VS Code extension: %s\n", ext)
		return nil
	}

	cmd := exec.Command("code", "--install-extension", ext)
	return cmd.Run()
}

func (e *macOSExecutor) copyDotfile(dotfile models.DotfileEntry, dryRun bool) error {
	src := dotfile.Source
	dst := dotfile.Destination

	// Ensure destination directory exists
	if !dryRun {
		os.MkdirAll(filepath.Dir(dst), 0755)
	}

	// Backup existing file
	if _, err := os.Stat(dst); err == nil && !dryRun {
		backup := dst + ".backup"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			os.Link(dst, backup)
		}
	}

	if dryRun {
		fmt.Printf("DRY-RUN: Would copy %s to %s\n", src, dst)
		return nil
	}

	// Copy file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func (e *macOSExecutor) applyDefault(domain, key, value, description string, dryRun bool) models.MigrationTask {
	task := models.MigrationTask{
		ID:       fmt.Sprintf("pref-%s-%s", domain, key),
		Name:     description,
		Category: "preferences",
		Action:   "configure",
		Status:   "success",
		Dry:      dryRun,
	}

	if dryRun {
		fmt.Printf("DRY-RUN: Would set defaults write %s %s %s\n", domain, key, value)
		task.ExecutedAt = time.Now()
		return task
	}

	cmd := exec.Command("defaults", "write", domain, key, value)
	if err := cmd.Run(); err != nil {
		task.Status = "failed"
		task.Error = err.Error()
	}

	task.ExecutedAt = time.Now()
	return task
}
