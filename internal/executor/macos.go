package executor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"wave/internal/models"
)

var shellIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

	return tasks, failedTasksError(tasks, "applications")
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

		if dryRun {
			if _, err := os.Stat(dotfile.Source); err != nil {
				task.Status = "skipped"
				task.Error = fmt.Sprintf("source unavailable: %v", err)
				task.ExecutedAt = time.Now()
				tasks = append(tasks, task)
				continue
			}
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

	return tasks, failedTasksError(tasks, "dotfiles")
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

	return tasks, failedTasksError(tasks, "preferences")
}

// ExecuteEnvironment configures shell environment
func (e *macOSExecutor) ExecuteEnvironment(env *models.EnvironmentGroup, dryRun bool) ([]models.MigrationTask, error) {
	var tasks []models.MigrationTask
	e.dryRun = dryRun

	shellProfile := env.ShellProfile
	if shellProfile == "" {
		shellProfile = filepath.Join(e.homeDir, ".zshrc")
	}
	if err := validatePathWithinHome(e.homeDir, shellProfile); err != nil {
		return nil, err
	}

	// Backup original
	backupPath := shellProfile + ".backup"
	if _, err := os.Stat(shellProfile); err == nil {
		if !dryRun {
			if backupInfo, err := os.Stat(backupPath); err == nil {
				profileInfo, err := os.Stat(shellProfile)
				if err != nil {
					return nil, err
				}
				if os.SameFile(backupInfo, profileInfo) {
					return nil, fmt.Errorf("shell profile backup is a hard link: %s", backupPath)
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			if err := copyFile(shellProfile, backupPath); err != nil {
				return nil, fmt.Errorf("back up shell profile: %w", err)
			}
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
		if !shellIdentifier.MatchString(key) {
			return nil, fmt.Errorf("invalid alias name: %s", key)
		}
		aliasSection += fmt.Sprintf("alias %s=%s\n", key, shellQuote(value))
	}

	// Add exports
	exportSection := "\n# Wave-managed exports\n"
	for key, value := range env.EnvironmentVars {
		if !strings.Contains(key, "PATH") { // Skip PATH for now
			if !shellIdentifier.MatchString(key) {
				return nil, fmt.Errorf("invalid environment variable name: %s", key)
			}
			exportSection += fmt.Sprintf("export %s=%s\n", key, shellQuote(value))
		}
	}

	// Write back
	if !dryRun && (len(env.Aliases) > 0 || len(env.EnvironmentVars) > 0) {
		newContent := content + aliasSection + exportSection
		if err := writeFileAtomically(shellProfile, []byte(newContent), 0600); err != nil {
			return nil, fmt.Errorf("write shell profile: %w", err)
		}
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

func validatePathWithinHome(homeDir, path string) error {
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	resolvedPath, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr != nil {
			return fmt.Errorf("resolve shell profile directory: %w", parentErr)
		}
		resolvedPath = filepath.Join(resolvedParent, filepath.Base(path))
	} else if err != nil {
		return fmt.Errorf("resolve shell profile: %w", err)
	}

	relative, err := filepath.Rel(resolvedHome, resolvedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("shell profile must be inside the home directory: %s", path)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func failedTasksError(tasks []models.MigrationTask, category string) error {
	var taskErrors []error
	for _, task := range tasks {
		if task.Status == "failed" {
			taskErrors = append(taskErrors, fmt.Errorf("%s: %s", task.Name, task.Error))
		}
	}
	if len(taskErrors) == 0 {
		return nil
	}
	return fmt.Errorf("one or more %s failed: %w", category, errors.Join(taskErrors...))
}

// ValidateState checks target device compatibility
func (e *macOSExecutor) ValidateState(state *models.MigrationState, dryRun bool) error {
	if state == nil {
		return fmt.Errorf("migration state is nil")
	}
	if state.Version == "" {
		return fmt.Errorf("migration state version is empty")
	}
	if !dryRun {
		for _, dotfile := range state.Dotfiles.Files {
			same, err := sameFile(dotfile.Source, dotfile.Destination)
			if err != nil {
				return fmt.Errorf("validate dotfile %s: %w", dotfile.Source, err)
			}
			if same {
				return fmt.Errorf("refusing to apply state whose source and destination are the same file: %s", dotfile.Source)
			}
		}
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

	if dryRun {
		fmt.Printf("DRY-RUN: Would copy %s to %s\n", src, dst)
		return nil
	}

	if same, err := sameFile(src, dst); err != nil {
		return err
	} else if same {
		return fmt.Errorf("source and destination are the same file: %s", src)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Backup existing file
	if _, err := os.Stat(dst); err == nil {
		backup := dst + ".backup"
		if backupInfo, err := os.Stat(backup); err == nil {
			destinationInfo, err := os.Stat(dst)
			if err != nil {
				return err
			}
			if os.SameFile(backupInfo, destinationInfo) {
				return fmt.Errorf("backup is a hard link to destination: %s", backup)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := copyFile(dst, backup); err != nil {
			return fmt.Errorf("back up destination: %w", err)
		}
	}

	return replaceFileAtomically(src, dst)
}

func sameFile(src, dst string) (bool, error) {
	srcPath, err := filepath.EvalSymlinks(src)
	if err != nil {
		return false, fmt.Errorf("resolve source: %w", err)
	}
	dstPath, err := filepath.EvalSymlinks(dst)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve destination: %w", err)
	}

	if filepath.Clean(srcPath) == filepath.Clean(dstPath) {
		return true, nil
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return false, err
	}
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return false, err
	}
	return os.SameFile(srcInfo, dstInfo), nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFileAtomically(dst, data, 0600)
}

func replaceFileAtomically(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	tempFile, err := os.CreateTemp(filepath.Dir(dst), ".wave-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, srcFile); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(0600); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, dst)
}

func writeFileAtomically(dst string, data []byte, mode os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(dst), ".wave-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, dst)
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
