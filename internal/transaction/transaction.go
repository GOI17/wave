package transaction

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	"wave/internal/bundle"
	"wave/internal/models"
)

const journalName = "journal.json"

// Journal records the durable state needed to rollback one apply.
type Journal struct {
	Version              int                 `json:"version"`
	ID                   string              `json:"id"`
	CreatedAt            time.Time           `json:"created_at"`
	Status               string              `json:"status"`
	Files                []FileJournal       `json:"files"`
	Preferences          []PreferenceJournal `json:"preferences,omitempty"`
	Packages             []PackageJournal    `json:"packages,omitempty"`
	System               []SystemJournal     `json:"system,omitempty"`
	Unresolved           []string            `json:"unresolved,omitempty"`
	RetainedApplications []string            `json:"retained_applications,omitempty"`
}

type PreferenceJournal struct {
	Domain       string `json:"domain"`
	Key          string `json:"key"`
	Existed      bool   `json:"existed"`
	BeforeType   string `json:"before_type,omitempty"`
	BeforeValue  string `json:"before_value,omitempty"`
	AppliedType  string `json:"applied_type"`
	AppliedValue string `json:"applied_value"`
	Applying     bool   `json:"applying"`
	Applied      bool   `json:"applied"`
	RollingBack  bool   `json:"rolling_back"`
	RolledBack   bool   `json:"rolled_back"`
}

type PackageJournal struct {
	Manager        string `json:"manager"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	AppliedVersion string `json:"applied_version,omitempty"`
	Applying       bool   `json:"applying"`
	Applied        bool   `json:"applied"`
	RollingBack    bool   `json:"rolling_back"`
	RolledBack     bool   `json:"rolled_back"`
}

type SystemJournal struct {
	Kind         string `json:"kind"`
	Before       string `json:"before"`
	AppliedValue string `json:"applied_value"`
	Applying     bool   `json:"applying"`
	Applied      bool   `json:"applied"`
	RollingBack  bool   `json:"rolling_back"`
	RolledBack   bool   `json:"rolled_back"`
}

// FileJournal records one applied file's before and after state.
type FileJournal struct {
	Destination      string      `json:"destination"`
	Existed          bool        `json:"existed"`
	Before           string      `json:"before,omitempty"`
	BeforeHash       string      `json:"before_hash,omitempty"`
	BeforeMode       os.FileMode `json:"before_mode,omitempty"`
	BeforeDevice     uint64      `json:"before_device,omitempty"`
	BeforeInode      uint64      `json:"before_inode,omitempty"`
	AppliedHash      string      `json:"applied_hash"`
	AppliedMode      os.FileMode `json:"applied_mode"`
	AppliedDevice    uint64      `json:"applied_device,omitempty"`
	AppliedInode     uint64      `json:"applied_inode,omitempty"`
	Staged           string      `json:"staged"`
	Applying         bool        `json:"applying"`
	Applied          bool        `json:"applied"`
	RolledBack       bool        `json:"rolled_back"`
	PreservedBefore  string      `json:"preserved_before,omitempty"`
	PreservedApplied string      `json:"preserved_applied,omitempty"`
}

// RollbackResult summarizes a conflict-safe rollback.
type RollbackResult struct {
	TransactionID        string   `json:"transaction_id"`
	Restored             int      `json:"restored"`
	Removed              int      `json:"removed"`
	PreferencesRestored  int      `json:"preferences_restored"`
	PackagesRemoved      int      `json:"packages_removed"`
	ApplicationsRetained int      `json:"applications_retained"`
	RetainedApplications []string `json:"retained_applications"`
	SystemRestored       int      `json:"system_restored"`
	Conflicts            int      `json:"conflicts"`
	ConflictPaths        []string `json:"conflict_paths"`
}

type System interface {
	Output(name string, args ...string) (string, error)
	Run(name string, args ...string) error
}

type executableSystem interface {
	LookPath(name string) error
}

type execSystem struct{}

func (execSystem) Output(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (execSystem) Run(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func (execSystem) LookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

// Preview returns a non-mutating summary of a portable bundle.
func Preview(bundlePath string) (*models.MigrationResult, error) {
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	for _, file := range opened.Manifest.Files {
		if _, err := opened.ReadFile(file); err != nil {
			return nil, err
		}
	}
	state := opened.Manifest.State
	result := &models.MigrationResult{DryRun: true, Warnings: []string{}}
	applicationCount := len(state.Applications.Homebrew) + len(state.Applications.VSCodeExtensions) + len(state.Applications.AppStore)
	unresolvedApplications := len(state.Applications.Manual)
	addPreviewCategory(result, "Applications", applicationCount, unresolvedApplications)
	addPreviewCategory(result, "Dotfiles", len(opened.Manifest.Files), 0)
	preferenceCount := countPreferences(state)
	addPreviewCategory(result, "Preferences", preferenceCount, 0)
	addPreviewCategory(result, "Environment", 0, 0)
	if unresolvedApplications > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d manual applications have no install payload and require manual installation", unresolvedApplications))
	}
	for _, warning := range previewPrerequisites(state, execSystem{}) {
		result.Warnings = append(result.Warnings, warning)
	}
	if state.Preferences.System.ComputerName != "" || state.Preferences.System.TimeZone != "" {
		result.Warnings = append(result.Warnings, "computer name and timezone changes require macOS administrator authorization")
	}
	if opened.Manifest.Excluded > 0 {
		result.Skipped += opened.Manifest.Excluded
		result.Total += opened.Manifest.Excluded
		result.Categories[1].Skipped += opened.Manifest.Excluded
		result.Categories[1].Total += opened.Manifest.Excluded
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d sensitive, unsafe, unavailable, or oversized files were excluded from capture", opened.Manifest.Excluded))
	}
	return result, nil
}

func addPreviewCategory(result *models.MigrationResult, name string, ready, skipped int) {
	result.Categories = append(result.Categories, models.MigrationCategoryResult{Name: name, Total: ready + skipped, Successful: ready, Skipped: skipped})
	result.Total += ready + skipped
	result.Successful += ready
	result.Skipped += skipped
}

func previewPrerequisites(state *models.MigrationState, system System) []string {
	lookup, ok := system.(executableSystem)
	if !ok {
		return nil
	}
	var warnings []string
	if len(state.Applications.Homebrew) > 0 && lookup.LookPath("brew") != nil {
		warnings = append(warnings, "Homebrew packages require the brew command on the target Mac")
	}
	if len(state.Applications.VSCodeExtensions) > 0 && lookup.LookPath("code") != nil {
		warnings = append(warnings, "VS Code extensions require the code command on the target Mac")
	}
	if len(state.Applications.AppStore) > 0 && lookup.LookPath("mas") != nil {
		warnings = append(warnings, "App Store applications require mas and an authenticated Apple ID")
	}
	return warnings
}

func validatePrerequisites(state *models.MigrationState, system System) error {
	lookup, ok := system.(executableSystem)
	if !ok {
		return nil
	}
	for _, requirement := range []struct {
		needed bool
		name   string
	}{
		{len(state.Applications.Homebrew) > 0, "brew"},
		{len(state.Applications.VSCodeExtensions) > 0, "code"},
		{len(state.Applications.AppStore) > 0, "mas"},
	} {
		if requirement.needed && lookup.LookPath(requirement.name) != nil {
			return fmt.Errorf("required command is unavailable: %s", requirement.name)
		}
	}
	if len(state.Applications.AppStore) > 0 {
		if _, err := system.Output("mas", "list"); err != nil {
			return fmt.Errorf("App Store migration requires mas authentication")
		}
	}
	return nil
}

// Latest returns the latest transaction that can still be rolled back.
func Latest(transactionsDir string) (*Journal, error) {
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return latestUnlocked(transactionsDir)
}

// Quarantine moves unreadable transaction metadata aside without deleting it.
func Quarantine(id, transactionsDir string) (string, error) {
	if !validTransactionID(id) || id == ".lock" {
		return "", fmt.Errorf("invalid transaction id")
	}
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	source := filepath.Join(transactionsDir, id)
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("transaction is not a directory")
	}
	quarantineDir := filepath.Join(filepath.Dir(transactionsDir), "quarantine")
	if err := os.MkdirAll(quarantineDir, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(quarantineDir, id+"-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := renamePaths(source, destination, 0x00000004); err != nil { // RENAME_EXCL
		return "", err
	}
	if err := syncDirectory(filepath.Dir(source)); err != nil {
		return "", err
	}
	if err := syncDirectory(quarantineDir); err != nil {
		return "", err
	}
	return destination, nil
}

func latestUnlocked(transactionsDir string) (*Journal, error) {
	entries, err := os.ReadDir(transactionsDir)
	if err != nil {
		return nil, err
	}
	var latest *Journal
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		journal, err := loadJournal(filepath.Join(transactionsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read transaction %s: %w", entry.Name(), err)
		}
		if journal.Status != "applied" && journal.Status != "rollback-conflicts" && journal.Status != "partial" && journal.Status != "preparing" {
			continue
		}
		if latest == nil || journal.CreatedAt.After(latest.CreatedAt) {
			latest = journal
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no rollback transaction is available")
	}
	return latest, nil
}

// RollbackLatest selects and rolls back the latest transaction under one lock.
func RollbackLatest(homeDir, transactionsDir string) (*RollbackResult, error) {
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	latest, err := latestUnlocked(transactionsDir)
	if err != nil {
		return nil, err
	}
	return rollbackUnlocked(latest.ID, homeDir, transactionsDir, execSystem{})
}

// FormatApplySummary renders the canonical apply completion summary.
func FormatApplySummary(journal *Journal) string {
	var summary strings.Builder
	summary.WriteString("Migration Apply Summary\n")
	summary.WriteString("=======================\n")
	fmt.Fprintf(&summary, "Transaction: %s\n", journal.ID)
	fmt.Fprintf(&summary, "Root dotfiles applied: %d\n", len(journal.Files))
	for _, file := range journal.Files {
		fmt.Fprintf(&summary, "- %s\n", file.Destination)
	}
	fmt.Fprintf(&summary, "Applications installed: %d\n", appliedPackages(journal.Packages))
	for _, item := range journal.Packages {
		if item.Applied {
			fmt.Fprintf(&summary, "- %s: %s\n", item.Manager, item.Name)
		}
	}
	fmt.Fprintf(&summary, "Preferences applied: %d\n", appliedPreferences(journal.Preferences))
	for _, item := range journal.Preferences {
		if item.Applied {
			fmt.Fprintf(&summary, "- %s:%s = %s\n", item.Domain, item.Key, item.AppliedValue)
		}
	}
	fmt.Fprintf(&summary, "System settings applied: %d\n", appliedSystem(journal.System))
	for _, item := range journal.System {
		if item.Applied {
			fmt.Fprintf(&summary, "- %s = %s\n", item.Kind, item.AppliedValue)
		}
	}
	if len(journal.Unresolved) > 0 {
		summary.WriteString("Unresolved items:\n")
		for _, item := range journal.Unresolved {
			fmt.Fprintf(&summary, "- %s\n", item)
		}
	}
	fmt.Fprintf(&summary, "\nRollback with: wave rollback --transaction %s --confirm\n", journal.ID)
	return summary.String()
}

func appliedPackages(items []PackageJournal) int {
	count := 0
	for _, item := range items {
		if item.Applied {
			count++
		}
	}
	return count
}

func appliedPreferences(items []PreferenceJournal) int {
	count := 0
	for _, item := range items {
		if item.Applied {
			count++
		}
	}
	return count
}

func appliedSystem(items []SystemJournal) int {
	count := 0
	for _, item := range items {
		if item.Applied {
			count++
		}
	}
	return count
}

// FormatRollbackSummary renders the canonical rollback completion summary.
func FormatRollbackSummary(result *RollbackResult) string {
	var summary strings.Builder
	summary.WriteString("Migration Rollback Summary\n")
	summary.WriteString("==========================\n")
	fmt.Fprintf(&summary, "Transaction: %s\n", result.TransactionID)
	fmt.Fprintf(&summary, "Files restored: %d\n", result.Restored)
	fmt.Fprintf(&summary, "Files removed: %d\n", result.Removed)
	fmt.Fprintf(&summary, "Preferences restored: %d\n", result.PreferencesRestored)
	fmt.Fprintf(&summary, "Applications removed: %d\n", result.PackagesRemoved)
	fmt.Fprintf(&summary, "Applications retained: %d\n", result.ApplicationsRetained)
	for _, application := range result.RetainedApplications {
		fmt.Fprintf(&summary, "- %s (manual cleanup if unwanted)\n", application)
	}
	fmt.Fprintf(&summary, "System settings restored: %d\n", result.SystemRestored)
	fmt.Fprintf(&summary, "Conflicts preserved: %d\n", result.Conflicts)
	if result.Conflicts > 0 {
		summary.WriteString("\nConflicts require manual resolution:\n")
		for _, path := range result.ConflictPaths {
			fmt.Fprintf(&summary, "- %s\n", path)
		}
	}
	return summary.String()
}

// Apply atomically applies file payloads and persists a write-ahead journal.
func Apply(bundlePath, homeDir, transactionsDir string) (*Journal, error) {
	return ApplyWithSystem(bundlePath, homeDir, transactionsDir, execSystem{})
}

func ApplyWithSystem(bundlePath, homeDir, transactionsDir string, system System) (*Journal, error) {
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return applyUnlocked(bundlePath, homeDir, transactionsDir, system)
}

func applyUnlocked(bundlePath, homeDir, transactionsDir string, system System) (*Journal, error) {
	if err := recoverUnfinished(homeDir, transactionsDir, system); err != nil {
		return nil, err
	}
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	if err := validatePrerequisites(opened.Manifest.State, system); err != nil {
		return nil, err
	}

	id, err := transactionID()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(transactionsDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "before"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "preserved"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "staged"), 0700); err != nil {
		return nil, err
	}
	waveDir := filepath.Dir(transactionsDir)
	if err := syncDirectory(filepath.Dir(waveDir)); err != nil {
		return nil, err
	}
	if err := syncDirectory(waveDir); err != nil {
		return nil, err
	}
	if err := syncDirectory(transactionsDir); err != nil {
		return nil, err
	}
	if err := syncDirectory(dir); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Join(dir, "before")); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Join(dir, "preserved")); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Join(dir, "staged")); err != nil {
		return nil, err
	}
	journal := &Journal{Version: 2, ID: id, CreatedAt: time.Now().UTC(), Status: "preparing", Files: []FileJournal{}, Preferences: []PreferenceJournal{}, Packages: []PackageJournal{}, System: []SystemJournal{}, Unresolved: []string{}}
	if err := saveJournal(dir, journal); err != nil {
		return nil, err
	}

	// Prepare and identify every candidate before mutating any destination.
	for i, file := range opened.Manifest.Files {
		destination, err := destinationPath(homeDir, file.Destination)
		if err != nil {
			return journal, err
		}
		data, err := opened.ReadFile(file)
		if err != nil {
			return journal, err
		}
		record := FileJournal{
			Destination: file.Destination,
			AppliedHash: file.SHA256,
			AppliedMode: file.Mode,
			Staged:      filepath.ToSlash(filepath.Join("staged", fmt.Sprintf("%06d", i))),
		}
		stagedPath := filepath.Join(dir, filepath.FromSlash(record.Staged))
		if err := writeAtomic(stagedPath, data, file.Mode); err != nil {
			return journal, err
		}
		_, _, record.AppliedDevice, record.AppliedInode, err = fileIdentity(stagedPath)
		if err != nil {
			return journal, err
		}
		linkInfo, linkErr := os.Lstat(destination)
		if linkErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 {
			return journal, fmt.Errorf("destination symlinks are not supported: %s", destination)
		}
		if linkErr != nil && !os.IsNotExist(linkErr) {
			return journal, linkErr
		}
		if info, err := os.Stat(destination); err == nil {
			if !info.Mode().IsRegular() {
				return journal, fmt.Errorf("destination is not a regular file: %s", destination)
			}
			record.Existed = true
			record.BeforeMode = info.Mode().Perm()
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return journal, fmt.Errorf("cannot identify original inode: %s", destination)
			}
			record.BeforeDevice = uint64(stat.Dev)
			record.BeforeInode = stat.Ino
			record.Before = filepath.ToSlash(filepath.Join("before", fmt.Sprintf("%06d", i)))
			record.PreservedBefore = filepath.ToSlash(filepath.Join("preserved", fmt.Sprintf("before-%06d", i)))
			beforeData, err := os.ReadFile(destination)
			if err != nil {
				return journal, err
			}
			beforeHash := sha256.Sum256(beforeData)
			record.BeforeHash = hex.EncodeToString(beforeHash[:])
			if err := writeAtomic(filepath.Join(dir, filepath.FromSlash(record.Before)), beforeData, 0600); err != nil {
				return journal, err
			}
		} else if !os.IsNotExist(err) {
			return journal, err
		}
		journal.Files = append(journal.Files, record)
	}
	if err := saveJournal(dir, journal); err != nil {
		return journal, err
	}

	for i := range journal.Files {
		record := &journal.Files[i]
		destination, err := destinationPath(homeDir, record.Destination)
		if err != nil {
			return recoverPartial(journal, homeDir, dir, system, err)
		}
		record.Applying = true
		if err := saveJournal(dir, journal); err != nil {
			return recoverPartial(journal, homeDir, dir, system, err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return recoverPartial(journal, homeDir, dir, system, err)
		}
		err = swapInFile(destination, *record, dir)
		if err != nil {
			return recoverPartial(journal, homeDir, dir, system, err)
		}
		record.Applied = true
		record.Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return recoverPartial(journal, homeDir, dir, system, err)
		}
	}
	if err := applyPreferences(journal, opened.Manifest.State, system, dir); err != nil {
		return recoverPartial(journal, homeDir, dir, system, err)
	}
	if err := applySystemSettings(journal, opened.Manifest.State, system, dir); err != nil {
		return recoverPartial(journal, homeDir, dir, system, err)
	}
	if err := applyPackages(journal, opened.Manifest.State, system, dir); err != nil {
		return recoverPartial(journal, homeDir, dir, system, err)
	}
	journal.Status = "applied"
	if err := saveJournal(dir, journal); err != nil {
		return journal, err
	}
	return journal, nil
}

// Rollback restores items that still match the state written by Apply.
func Rollback(id, homeDir, transactionsDir string) (*RollbackResult, error) {
	return RollbackWithSystem(id, homeDir, transactionsDir, execSystem{})
}

func RollbackWithSystem(id, homeDir, transactionsDir string, system System) (*RollbackResult, error) {
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return rollbackUnlocked(id, homeDir, transactionsDir, system)
}

func rollbackUnlocked(id, homeDir, transactionsDir string, system System) (*RollbackResult, error) {
	if !validTransactionID(id) {
		return nil, fmt.Errorf("invalid transaction id")
	}
	dir := filepath.Join(transactionsDir, id)
	journal, err := loadJournal(dir)
	if err != nil {
		return nil, err
	}
	if journal.Status != "applied" && journal.Status != "rollback-conflicts" && journal.Status != "partial" && journal.Status != "preparing" {
		return nil, fmt.Errorf("transaction cannot be rolled back from status %s", journal.Status)
	}
	return rollbackJournal(journal, homeDir, dir, system)
}

func rollbackJournal(journal *Journal, homeDir, dir string, system System) (*RollbackResult, error) {
	result := &RollbackResult{TransactionID: journal.ID, ConflictPaths: []string{}, RetainedApplications: append([]string(nil), journal.RetainedApplications...), ApplicationsRetained: len(journal.RetainedApplications)}
	for i := len(journal.Packages) - 1; i >= 0; i-- {
		record := &journal.Packages[i]
		if record.RolledBack {
			continue
		}
		if record.Applying && !record.Applied {
			_, installed, probeErr := packageStatus(system, record.Manager, record.Name, record.Type)
			if probeErr != nil {
				addRollbackConflict(journal, result, dir, "application state unknown:"+record.Name)
				continue
			}
			if installed {
				record.Applied, record.Applying = true, false
			} else {
				record.RolledBack = true
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
		}
		if !record.Applied {
			continue
		}
		record.RolledBack = true
		retained := record.Manager + ":" + record.Name
		if !containsString(journal.RetainedApplications, retained) {
			journal.RetainedApplications = append(journal.RetainedApplications, retained)
			result.ApplicationsRetained++
			result.RetainedApplications = append(result.RetainedApplications, retained)
		}
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
	}
	for i := len(journal.System) - 1; i >= 0; i-- {
		record := &journal.System[i]
		if record.RolledBack {
			continue
		}
		current, err := readSystemSetting(system, record.Kind)
		if record.RollingBack && err == nil && current == record.Before {
			record.RollingBack, record.RolledBack = false, true
			result.SystemRestored++
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			continue
		}
		if !record.Applied && record.Applying {
			if err == nil && current == record.Before {
				record.RolledBack = true
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
			if err == nil && current == record.AppliedValue {
				record.Applied, record.Applying = true, false
			} else {
				addRollbackConflict(journal, result, dir, "system mutation interrupted:"+record.Kind)
				continue
			}
		}
		if !record.Applied {
			addRollbackConflict(journal, result, dir, "system:"+record.Kind)
			continue
		}
		if err != nil || current != record.AppliedValue {
			addRollbackConflict(journal, result, dir, "system:"+record.Kind)
			continue
		}
		record.RollingBack = true
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
		if err := writeSystemSetting(system, record.Kind, record.Before); err != nil {
			record.RollingBack = false
			addRollbackConflict(journal, result, dir, "system restore failed:"+record.Kind)
			continue
		}
		record.RollingBack, record.RolledBack = false, true
		result.SystemRestored++
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
	}
	for i := len(journal.Preferences) - 1; i >= 0; i-- {
		record := &journal.Preferences[i]
		if record.RolledBack {
			continue
		}
		current, err := readPreference(system, record.Domain, record.Key)
		matchesBefore := err == nil && current.Existed == record.Existed && (!current.Existed || current.Type == record.BeforeType && current.Value == record.BeforeValue)
		if record.RollingBack && matchesBefore {
			record.RollingBack, record.RolledBack = false, true
			result.PreferencesRestored++
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			continue
		}
		if !record.Applied && record.Applying {
			if matchesBefore {
				record.RolledBack = true
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
			if err == nil && current.Existed && current.Type == record.AppliedType && current.Value == record.AppliedValue {
				record.Applied, record.Applying = true, false
			}
		}
		if !record.Applied {
			addRollbackConflict(journal, result, dir, "preference:"+record.Domain+":"+record.Key)
			continue
		}
		if err != nil || !current.Existed || current.Type != record.AppliedType || current.Value != record.AppliedValue {
			addRollbackConflict(journal, result, dir, "preference:"+record.Domain+":"+record.Key)
			continue
		}
		record.RollingBack = true
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
		if record.Existed {
			if err := writePreference(system, record.Domain, record.Key, record.BeforeType, record.BeforeValue); err != nil {
				record.RollingBack = false
				addRollbackConflict(journal, result, dir, "preference restore failed:"+record.Domain+":"+record.Key)
				continue
			}
		} else if err := system.Run("defaults", "delete", record.Domain, record.Key); err != nil {
			record.RollingBack = false
			addRollbackConflict(journal, result, dir, "preference restore failed:"+record.Domain+":"+record.Key)
			continue
		}
		record.RollingBack, record.RolledBack = false, true
		result.PreferencesRestored++
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
	}
	for i := len(journal.Files) - 1; i >= 0; i-- {
		record := &journal.Files[i]
		if record.RolledBack {
			continue
		}
		destination, err := destinationPath(homeDir, record.Destination)
		if err != nil {
			addRollbackConflict(journal, result, dir, "file path failed:"+record.Destination)
			continue
		}
		currentHash, currentMode, currentDevice, currentInode, err := fileIdentity(destination)
		if !record.Applied {
			if err == nil && record.Existed && currentHash == record.BeforeHash && currentMode == record.BeforeMode && currentDevice == record.BeforeDevice && currentInode == record.BeforeInode {
				record.RolledBack = true
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
			if os.IsNotExist(err) && !record.Existed {
				record.RolledBack = true
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
			if record.Applying && err == nil && currentHash == record.AppliedHash && currentMode == record.AppliedMode && currentDevice == record.AppliedDevice && currentInode == record.AppliedInode {
				record.Applied = true
				record.Applying = false
			} else {
				result.Conflicts++
				result.ConflictPaths = append(result.ConflictPaths, record.Destination)
				journal.Status = "rollback-conflicts"
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
		}
		if err == nil && record.Existed && currentHash == record.BeforeHash && currentMode == record.BeforeMode && currentDevice == record.BeforeDevice && currentInode == record.BeforeInode {
			record.RolledBack = true
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			continue
		}
		if os.IsNotExist(err) && !record.Existed {
			if record.PreservedApplied != "" {
				preserved := filepath.Join(dir, filepath.FromSlash(record.PreservedApplied))
				_, _, device, inode, preservedErr := fileIdentity(preserved)
				if preservedErr != nil || device != record.AppliedDevice || inode != record.AppliedInode {
					result.Conflicts++
					result.ConflictPaths = append(result.ConflictPaths, record.Destination)
					journal.Status = "rollback-conflicts"
					if err := saveJournal(dir, journal); err != nil {
						return result, err
					}
					continue
				}
			}
			record.RolledBack = true
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			continue
		}
		if err != nil || currentHash != record.AppliedHash || currentMode != record.AppliedMode || currentDevice != record.AppliedDevice || currentInode != record.AppliedInode {
			result.Conflicts++
			result.ConflictPaths = append(result.ConflictPaths, record.Destination)
			journal.Status = "rollback-conflicts"
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			continue
		}
		if !record.Existed {
			record.PreservedApplied = filepath.ToSlash(filepath.Join("preserved", fmt.Sprintf("applied-%06d", i)))
			if err := saveJournal(dir, journal); err != nil {
				return result, err
			}
			preserved := filepath.Join(dir, filepath.FromSlash(record.PreservedApplied))
			if err := os.Rename(destination, preserved); err != nil {
				addRollbackConflict(journal, result, dir, "file restore failed:"+record.Destination)
				continue
			}
			if err := syncDirectory(filepath.Dir(destination)); err != nil {
				addRollbackConflict(journal, result, dir, "file sync failed:"+record.Destination)
				continue
			}
			if err := syncDirectory(filepath.Dir(preserved)); err != nil {
				addRollbackConflict(journal, result, dir, "transaction sync failed:"+record.Destination)
				continue
			}
			_, _, device, inode, err := fileIdentity(preserved)
			if err != nil || device != record.AppliedDevice || inode != record.AppliedInode {
				if restoreErr := renameNoReplace(preserved, destination); restoreErr != nil {
					journal.Status = "rollback-conflicts"
					if saveErr := saveJournal(dir, journal); saveErr != nil {
						return result, saveErr
					}
					return result, fmt.Errorf("destination changed during rollback; concurrent file preserved at %s", preserved)
				}
				record.PreservedApplied = ""
				result.Conflicts++
				result.ConflictPaths = append(result.ConflictPaths, record.Destination)
				journal.Status = "rollback-conflicts"
				if err := saveJournal(dir, journal); err != nil {
					return result, err
				}
				continue
			}
			result.Removed++
		} else {
			if err := swapRollbackFile(destination, dir, record); err != nil {
				addRollbackConflict(journal, result, dir, "file restore failed:"+record.Destination)
				continue
			}
			record.PreservedApplied = record.Staged
			result.Restored++
		}
		record.RolledBack = true
		if err := saveJournal(dir, journal); err != nil {
			return result, err
		}
	}
	if result.Conflicts > 0 {
		journal.Status = "rollback-conflicts"
	} else {
		journal.Status = "rolled-back"
	}
	if err := saveJournal(dir, journal); err != nil {
		return result, err
	}
	return result, nil
}

func addRollbackConflict(journal *Journal, result *RollbackResult, dir, path string) {
	result.Conflicts++
	result.ConflictPaths = append(result.ConflictPaths, path)
	journal.Status = "rollback-conflicts"
	_ = saveJournal(dir, journal)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func recoverPartial(journal *Journal, homeDir, dir string, system System, applyErr error) (*Journal, error) {
	journal.Status = "partial"
	_ = saveJournal(dir, journal)
	result, rollbackErr := rollbackJournal(journal, homeDir, dir, system)
	if rollbackErr != nil || result.Conflicts > 0 {
		return journal, fmt.Errorf("apply failed: %w; automatic rollback incomplete: %s", applyErr, strings.TrimSpace(FormatRollbackSummary(result)))
	}
	if result.ApplicationsRetained > 0 {
		return journal, fmt.Errorf("apply failed: %w; files and settings were rolled back; %s", applyErr, strings.TrimSpace(FormatRollbackSummary(result)))
	}
	return journal, fmt.Errorf("apply failed and was rolled back: %w", applyErr)
}

func recoverUnfinished(homeDir, transactionsDir string, system System) error {
	entries, err := os.ReadDir(transactionsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(transactionsDir, entry.Name())
		journal, err := loadJournal(dir)
		if err != nil {
			return fmt.Errorf("read transaction %s: %w", entry.Name(), err)
		}
		if journal.Status != "preparing" && journal.Status != "partial" {
			if journal.Status == "rollback-conflicts" {
				return fmt.Errorf("unfinished transaction %s requires rollback resolution", journal.ID)
			}
			continue
		}
		result, err := rollbackJournal(journal, homeDir, dir, system)
		if err != nil || result.Conflicts > 0 {
			return fmt.Errorf("unfinished transaction %s requires rollback resolution", journal.ID)
		}
		if result.ApplicationsRetained > 0 {
			return fmt.Errorf("unfinished transaction %s recovered with retained applications; review transaction before continuing: %s", journal.ID, strings.TrimSpace(FormatRollbackSummary(result)))
		}
	}
	return nil
}

type preferenceValue struct {
	Existed bool
	Type    string
	Value   string
}

type preferenceSpec struct {
	Domain string
	Key    string
	Type   string
	Value  string
}

func applyPreferences(journal *Journal, state *models.MigrationState, system System, dir string) error {
	for _, spec := range preferenceSpecs(state) {
		before, err := readPreference(system, spec.Domain, spec.Key)
		if err != nil {
			return err
		}
		record := PreferenceJournal{Domain: spec.Domain, Key: spec.Key, Existed: before.Existed, BeforeType: before.Type, BeforeValue: before.Value, AppliedType: spec.Type, AppliedValue: spec.Value, Applying: true}
		journal.Preferences = append(journal.Preferences, record)
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		if err := writePreference(system, spec.Domain, spec.Key, spec.Type, spec.Value); err != nil {
			return err
		}
		journal.Preferences[len(journal.Preferences)-1].Applied = true
		journal.Preferences[len(journal.Preferences)-1].Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	return nil
}

func preferenceSpecs(state *models.MigrationState) []preferenceSpec {
	prefs := state.Preferences
	if preferencesEmpty(prefs) {
		return nil
	}
	specs := []preferenceSpec{
		{Domain: "com.apple.finder", Key: "AppleShowAllFiles", Type: "bool", Value: strconv.FormatBool(prefs.Finder.ShowHiddenFiles)},
		{Domain: "com.apple.dock", Key: "autohide", Type: "bool", Value: strconv.FormatBool(prefs.Dock.Autohide)},
		{Domain: "com.apple.dock", Key: "show-recents", Type: "bool", Value: strconv.FormatBool(prefs.Dock.ShowRecents)},
	}
	if prefs.Finder.DefaultViewMode != "" {
		specs = append(specs, preferenceSpec{Domain: "com.apple.finder", Key: "FXPreferredViewStyle", Type: "string", Value: prefs.Finder.DefaultViewMode})
	}
	if prefs.Dock.Position != "" {
		specs = append(specs, preferenceSpec{Domain: "com.apple.dock", Key: "orientation", Type: "string", Value: prefs.Dock.Position})
	}
	if prefs.Keyboard.KeyRepeat > 0 {
		specs = append(specs, preferenceSpec{Domain: "-g", Key: "KeyRepeat", Type: "int", Value: strconv.Itoa(prefs.Keyboard.KeyRepeat)})
	}
	if prefs.Keyboard.InitialRepeat > 0 {
		specs = append(specs, preferenceSpec{Domain: "-g", Key: "InitialKeyRepeat", Type: "int", Value: strconv.Itoa(prefs.Keyboard.InitialRepeat)})
	}
	return specs
}

func preferencesEmpty(prefs models.PreferencesGroup) bool {
	return prefs.Finder == (models.FinderPrefs{}) && prefs.Dock.Position == "" && !prefs.Dock.Autohide && !prefs.Dock.ShowRecents && len(prefs.Dock.AppOrder) == 0 && len(prefs.Dock.PersistentApps) == 0 && prefs.Keyboard == (models.KeyboardPrefs{}) && prefs.Trackpad == (models.TrackpadPrefs{}) && prefs.System == (models.SystemPrefs{}) && len(prefs.Apps) == 0
}

func readPreference(system System, domain, key string) (preferenceValue, error) {
	value, err := system.Output("defaults", "read", domain, key)
	if err != nil {
		return preferenceValue{}, nil
	}
	typeOutput, err := system.Output("defaults", "read-type", domain, key)
	if err != nil {
		return preferenceValue{}, err
	}
	kind := preferenceType(typeOutput)
	return preferenceValue{Existed: true, Type: kind, Value: normalizePreference(kind, value)}, nil
}

func preferenceType(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "boolean"):
		return "bool"
	case strings.Contains(lower, "integer"):
		return "int"
	case strings.Contains(lower, "float"):
		return "float"
	default:
		return "string"
	}
}

func normalizePreference(kind, value string) string {
	value = strings.TrimSpace(value)
	if kind == "bool" {
		return strconv.FormatBool(value == "1" || strings.EqualFold(value, "true"))
	}
	return value
}

func writePreference(system System, domain, key, kind, value string) error {
	return system.Run("defaults", "write", domain, key, "-"+kind, value)
}

func applySystemSettings(journal *Journal, state *models.MigrationState, system System, dir string) error {
	settings := []SystemJournal{
		{Kind: "computer-name", AppliedValue: state.Preferences.System.ComputerName},
		{Kind: "time-zone", AppliedValue: state.Preferences.System.TimeZone},
		{Kind: "language", AppliedValue: state.Preferences.System.Language},
	}
	for _, record := range settings {
		if record.AppliedValue == "" {
			continue
		}
		before, err := readSystemSetting(system, record.Kind)
		if err != nil {
			return err
		}
		record.Before = before
		record.Applying = true
		journal.System = append(journal.System, record)
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		if err := writeSystemSetting(system, record.Kind, record.AppliedValue); err != nil {
			return err
		}
		journal.System[len(journal.System)-1].Applied = true
		journal.System[len(journal.System)-1].Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	return nil
}

func readSystemSetting(system System, kind string) (string, error) {
	switch kind {
	case "computer-name":
		return system.Output("scutil", "--get", "ComputerName")
	case "time-zone":
		value, err := system.Output("readlink", "/etc/localtime")
		const marker = "/zoneinfo/"
		if index := strings.Index(value, marker); index >= 0 {
			value = value[index+len(marker):]
		}
		return value, err
	case "language":
		value, err := system.Output("defaults", "read", "-g", "AppleLanguages")
		return normalizeLanguage(value), err
	default:
		return "", fmt.Errorf("unsupported system setting: %s", kind)
	}
}

func writeSystemSetting(system System, kind, value string) error {
	switch kind {
	case "computer-name":
		return runAuthorized(system, "/usr/sbin/scutil", "--set", "ComputerName", value)
	case "time-zone":
		return runAuthorized(system, "/usr/sbin/systemsetup", "-settimezone", value)
	case "language":
		args := []string{"write", "-g", "AppleLanguages", "-array"}
		args = append(args, strings.Split(value, ",")...)
		return system.Run("defaults", args...)
	default:
		return fmt.Errorf("unsupported system setting: %s", kind)
	}
}

func normalizeLanguage(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "() \n\t")
	var languages []string
	for _, language := range strings.Split(value, ",") {
		language = strings.Trim(strings.TrimSpace(language), `"`)
		if language != "" {
			languages = append(languages, language)
		}
	}
	return strings.Join(languages, ",")
}

func runAuthorized(system System, executable string, args ...string) error {
	quoted := []string{shellQuote(executable)}
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	script := fmt.Sprintf(`do shell script %q with administrator privileges`, strings.Join(quoted, " "))
	return system.Run("osascript", "-e", script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func applyPackages(journal *Journal, state *models.MigrationState, system System, dir string) error {
	for _, app := range state.Applications.Manual {
		journal.Unresolved = append(journal.Unresolved, fmt.Sprintf("manual application: %s (%s)", app.Name, app.Path))
	}
	for _, pkg := range state.Applications.Homebrew {
		if !validPackageName(pkg.Name) || (pkg.Type != "formula" && pkg.Type != "cask") {
			return fmt.Errorf("invalid Homebrew package: %s", pkg.Name)
		}
		if _, installed, err := packageStatus(system, "homebrew", pkg.Name, pkg.Type); err != nil {
			return fmt.Errorf("inspect Homebrew %s %s: %w", pkg.Type, pkg.Name, err)
		} else if installed {
			continue
		}
		record := PackageJournal{Manager: "homebrew", Name: pkg.Name, Type: pkg.Type, Applying: true}
		journal.Packages = append(journal.Packages, record)
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		args := []string{"install"}
		if pkg.Type == "cask" {
			args = append(args, "--cask")
		}
		if err := system.Run("brew", append(args, pkg.Name)...); err != nil {
			return fmt.Errorf("install Homebrew %s %s: %w", pkg.Type, pkg.Name, err)
		}
		version, installed, err := packageStatus(system, "homebrew", pkg.Name, pkg.Type)
		if err != nil || !installed {
			return fmt.Errorf("verify Homebrew %s %s after install", pkg.Type, pkg.Name)
		}
		record = journal.Packages[len(journal.Packages)-1]
		record.Applying, record.Applied, record.AppliedVersion = false, true, version
		journal.Packages[len(journal.Packages)-1] = record
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	for _, extension := range state.Applications.VSCodeExtensions {
		if !validExtensionID(extension) {
			return fmt.Errorf("invalid VS Code extension: %s", extension)
		}
		if _, installed, err := packageStatus(system, "vscode", extension, "extension"); err != nil {
			return fmt.Errorf("inspect VS Code extension %s: %w", extension, err)
		} else if installed {
			continue
		}
		journal.Packages = append(journal.Packages, PackageJournal{Manager: "vscode", Name: extension, Type: "extension", Applying: true})
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		if err := system.Run("code", "--install-extension", extension); err != nil {
			return fmt.Errorf("install VS Code extension %s: %w", extension, err)
		}
		journal.Packages[len(journal.Packages)-1].Applied = true
		journal.Packages[len(journal.Packages)-1].Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	for _, app := range state.Applications.AppStore {
		if !regexp.MustCompile(`^[0-9]+$`).MatchString(app.BundleID) {
			journal.Unresolved = append(journal.Unresolved, fmt.Sprintf("App Store application: %s (invalid mas ID %s)", app.Name, app.BundleID))
			continue
		}
		if _, installed, err := packageStatus(system, "mas", app.BundleID, "app-store"); err != nil {
			return fmt.Errorf("inspect App Store application %s: %w", app.Name, err)
		} else if installed {
			continue
		}
		journal.Packages = append(journal.Packages, PackageJournal{Manager: "mas", Name: app.BundleID, Type: "app-store", Applying: true})
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		if err := system.Run("mas", "install", app.BundleID); err != nil {
			return fmt.Errorf("install App Store application %s: %w", app.Name, err)
		}
		journal.Packages[len(journal.Packages)-1].Applied = true
		journal.Packages[len(journal.Packages)-1].Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	return saveJournal(dir, journal)
}

var packageName = regexp.MustCompile(`^[A-Za-z0-9@+_.-]+(?:/[A-Za-z0-9@+_.-]+)*$`)
var extensionID = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]+$`)

func validPackageName(value string) bool { return packageName.MatchString(value) }
func validExtensionID(value string) bool { return extensionID.MatchString(value) }

func packageStatus(system System, manager, name, kind string) (string, bool, error) {
	switch manager {
	case "vscode":
		output, err := system.Output("code", "--list-extensions")
		if err != nil {
			return "", false, err
		}
		for _, extension := range strings.Split(output, "\n") {
			if strings.EqualFold(strings.TrimSpace(extension), name) {
				return "", true, nil
			}
		}
		return "", false, nil
	case "mas":
		output, err := system.Output("mas", "list")
		if err != nil {
			return "", false, err
		}
		return "", strings.HasPrefix(output, name+" ") || strings.Contains(output, "\n"+name+" "), nil
	default:
		args := []string{"list", "--formula"}
		if kind == "cask" {
			args = []string{"list", "--cask"}
		}
		output, err := system.Output("brew", args...)
		if err != nil {
			return "", false, err
		}
		for _, installed := range strings.Fields(output) {
			if installed == name {
				return name, true, nil
			}
		}
		return "", false, nil
	}
}

func swapInFile(destination string, record FileJournal, transactionDir string) error {
	staged := filepath.Join(transactionDir, filepath.FromSlash(record.Staged))
	if hash, mode, device, inode, err := fileIdentity(staged); err != nil || hash != record.AppliedHash || mode != record.AppliedMode || device != record.AppliedDevice || inode != record.AppliedInode {
		return fmt.Errorf("staged candidate does not match journal: %s", record.Destination)
	}
	if !record.Existed {
		if err := os.Link(staged, destination); err != nil {
			return fmt.Errorf("destination appeared during apply: %s", record.Destination)
		}
		return syncDirectory(filepath.Dir(destination))
	}
	if err := swapPaths(destination, staged); err != nil {
		return err
	}
	hash, oldMode, oldDevice, oldInode, err := fileIdentity(staged)
	if err != nil || hash != record.BeforeHash || oldMode != record.BeforeMode || oldDevice != record.BeforeDevice || oldInode != record.BeforeInode {
		displacedHash := hash
		displacedMode := oldMode
		if restoreErr := swapPaths(destination, staged); restoreErr != nil {
			return fmt.Errorf("destination changed during apply; displaced file preserved at %s: %w", staged, restoreErr)
		}
		restoredHash, restoredMode, restoreErr := fileStateRegular(destination)
		if restoreErr != nil || restoredHash != displacedHash || restoredMode != displacedMode {
			return fmt.Errorf("could not verify restored concurrent file; candidate preserved at %s", staged)
		}
		return fmt.Errorf("destination changed during apply; candidate preserved at %s", staged)
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("apply swap was not durably synced; displaced file preserved at %s: %w", staged, err)
	}
	return syncDirectory(filepath.Dir(staged))
}

func swapRollbackFile(destination, transactionDir string, record *FileJournal) error {
	preserved := filepath.Join(transactionDir, filepath.FromSlash(record.Staged))
	beforeHash, beforeMode, beforeDevice, beforeInode, err := fileIdentity(preserved)
	if err != nil || beforeHash != record.BeforeHash || beforeMode != record.BeforeMode || beforeDevice != record.BeforeDevice || beforeInode != record.BeforeInode {
		return fmt.Errorf("preserved original does not match journal: %s", record.Destination)
	}
	if err := swapPaths(destination, preserved); err != nil {
		return err
	}
	hash, appliedMode, appliedDevice, appliedInode, err := fileIdentity(preserved)
	if err != nil || hash != record.AppliedHash || appliedMode != record.AppliedMode || appliedDevice != record.AppliedDevice || appliedInode != record.AppliedInode {
		displacedHash := hash
		displacedMode := appliedMode
		if restoreErr := swapPaths(destination, preserved); restoreErr != nil {
			return fmt.Errorf("destination changed during rollback; displaced file preserved at %s: %w", preserved, restoreErr)
		}
		restoredHash, restoredMode, restoreErr := fileStateRegular(destination)
		if restoreErr != nil || restoredHash != displacedHash || restoredMode != displacedMode {
			return fmt.Errorf("could not verify restored concurrent file; rollback candidate preserved at %s", preserved)
		}
		return fmt.Errorf("destination changed during rollback; candidate preserved at %s", preserved)
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("rollback swap was not durably synced; applied file preserved at %s: %w", preserved, err)
	}
	return syncDirectory(filepath.Dir(preserved))
}

func swapPaths(first, second string) error {
	return renamePaths(first, second, 0x00000002) // RENAME_SWAP
}

func renameExclusive(first, second string) error {
	return renamePaths(first, second, 0x00000004|0x00000010) // RENAME_EXCL | RENAME_NOFOLLOW_ANY
}

func renameNoReplace(first, second string) error {
	return renamePaths(first, second, 0x00000004) // RENAME_EXCL
}

func renamePaths(first, second string, flags uintptr) error {
	atFDCWD := int32(-2)
	firstPointer, err := syscall.BytePtrFromString(first)
	if err != nil {
		return err
	}
	secondPointer, err := syscall.BytePtrFromString(second)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(
		unix.SYS_RENAMEATX_NP,
		uintptr(atFDCWD),
		uintptr(unsafe.Pointer(firstPointer)),
		uintptr(atFDCWD),
		uintptr(unsafe.Pointer(secondPointer)),
		flags,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func countPreferences(state *models.MigrationState) int {
	prefs := state.Preferences
	if preferencesEmpty(prefs) {
		return 0
	}
	count := 3 // Finder hidden files, Dock autohide, Dock recents.
	if prefs.Finder.DefaultViewMode != "" {
		count++
	}
	if prefs.Dock.Position != "" {
		count++
	}
	if prefs.Keyboard.KeyRepeat > 0 {
		count++
	}
	if prefs.Keyboard.InitialRepeat > 0 {
		count++
	}
	if prefs.System.ComputerName != "" {
		count++
	}
	if prefs.System.TimeZone != "" {
		count++
	}
	if prefs.System.Language != "" {
		count++
	}
	return count
}

func acquireLock(transactionsDir string) (func(), error) {
	if err := os.MkdirAll(transactionsDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(transactionsDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another apply or rollback operation is running")
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func transactionID() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
}

func validTransactionID(id string) bool {
	return id != "" && id != "." && id != ".." && !filepath.IsAbs(id) && !strings.ContainsAny(id, `/\`) && filepath.Base(id) == id
}

func destinationPath(homeDir, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe destination: %s", relative)
	}
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(resolvedHome, clean)
	resolvedParent, err := resolveExistingParent(filepath.Dir(destination))
	if err != nil {
		return "", err
	}
	parentRelative, err := filepath.Rel(resolvedHome, resolvedParent)
	if err != nil || parentRelative == ".." || strings.HasPrefix(parentRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination escapes target home: %s", relative)
	}
	return destination, nil
}

func resolveExistingParent(path string) (string, error) {
	current := path
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			remainder, relErr := filepath.Rel(current, path)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Join(resolved, remainder), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}

func saveJournal(dir string, journal *Journal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, journalName), data, 0600)
}

func loadJournal(dir string) (*Journal, error) {
	data, err := os.ReadFile(filepath.Join(dir, journalName))
	if err != nil {
		return nil, err
	}
	journal := &Journal{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(journal); err != nil {
		migrated, migrateErr := migrateLegacyJournal(dir, data)
		if migrateErr != nil {
			return nil, err
		}
		journal = migrated
	}
	if journal.Version == 0 {
		migrated, err := migrateLegacyJournal(dir, data)
		if err != nil {
			return nil, err
		}
		journal = migrated
	}
	if journal.Version != 1 && journal.Version != 2 {
		return nil, fmt.Errorf("unsupported transaction journal version")
	}
	if err := validateJournal(dir, journal); err != nil {
		return nil, err
	}
	return journal, nil
}

func migrateLegacyJournal(dir string, data []byte) (*Journal, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"id": true, "created_at": true, "status": true, "files": true}
	for key := range raw {
		if !allowed[key] {
			return nil, fmt.Errorf("unsupported legacy transaction field: %s", key)
		}
	}
	legacy := &Journal{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(legacy); err != nil {
		return nil, err
	}
	legacy.Version = 1
	if len(legacy.Files) > 0 {
		return nil, fmt.Errorf("legacy file-changing journal requires quarantine and manual recovery")
	}
	if err := validateJournal(dir, legacy); err != nil {
		return nil, err
	}
	if err := saveJournal(dir, legacy); err != nil {
		return nil, err
	}
	return legacy, nil
}

func validateJournal(dir string, journal *Journal) error {
	if journal.ID == "" || journal.ID != filepath.Base(dir) || journal.CreatedAt.IsZero() {
		return fmt.Errorf("invalid transaction identity")
	}
	validStatus := map[string]bool{"preparing": true, "partial": true, "applied": true, "rollback-conflicts": true, "rolled-back": true}
	if !validStatus[journal.Status] {
		return fmt.Errorf("invalid transaction status")
	}
	destinations := make(map[string]bool)
	for i, file := range journal.Files {
		if file.Destination == "" || !immediateJournalDotfile(file.Destination) || destinations[file.Destination] {
			return fmt.Errorf("invalid transaction destination")
		}
		destinations[file.Destination] = true
		if !validHash(file.AppliedHash) || file.AppliedMode.Perm() == 0 || file.AppliedMode.Perm() > 0777 {
			return fmt.Errorf("invalid applied file state")
		}
		expectedStaged := filepath.ToSlash(filepath.Join("staged", fmt.Sprintf("%06d", i)))
		if file.Staged != expectedStaged || file.AppliedDevice == 0 || file.AppliedInode == 0 {
			return fmt.Errorf("invalid staged file state")
		}
		if file.Existed {
			expectedBefore := filepath.ToSlash(filepath.Join("before", fmt.Sprintf("%06d", i)))
			expectedPreserved := filepath.ToSlash(filepath.Join("preserved", fmt.Sprintf("before-%06d", i)))
			if file.Before != expectedBefore || file.PreservedBefore != expectedPreserved || !validHash(file.BeforeHash) || file.BeforeMode.Perm() == 0 || file.BeforeMode.Perm() > 0777 || file.BeforeDevice == 0 || file.BeforeInode == 0 {
				return fmt.Errorf("invalid before file state")
			}
			beforePath := filepath.Join(dir, filepath.FromSlash(file.Before))
			hash, mode, err := fileState(beforePath)
			if err != nil || hash != file.BeforeHash || mode != 0600 {
				return fmt.Errorf("transaction backup does not match journal")
			}
			preservedPath := filepath.Join(dir, filepath.FromSlash(file.Staged))
			preservedInfo, err := os.Lstat(preservedPath)
			if err != nil || !preservedInfo.Mode().IsRegular() {
				return fmt.Errorf("preserved transaction inode is missing")
			}
			stat, ok := preservedInfo.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("cannot identify preserved transaction inode")
			}
			preservedDevice := uint64(stat.Dev)
			preservedInode := stat.Ino
			matchesBefore := preservedDevice == file.BeforeDevice && preservedInode == file.BeforeInode
			matchesApplied := file.AppliedDevice != 0 && preservedDevice == file.AppliedDevice && preservedInode == file.AppliedInode
			if !matchesBefore && !matchesApplied {
				return fmt.Errorf("preserved inode does not match transaction journal")
			}
		} else if file.Before != "" || file.BeforeHash != "" || file.BeforeMode != 0 || file.BeforeDevice != 0 || file.BeforeInode != 0 || file.PreservedBefore != "" {
			return fmt.Errorf("unexpected before state for new file")
		}
		if !file.Existed {
			expectedPreservedApplied := filepath.ToSlash(filepath.Join("preserved", fmt.Sprintf("applied-%06d", i)))
			if file.PreservedApplied != "" && file.PreservedApplied != expectedPreservedApplied {
				return fmt.Errorf("invalid preserved applied path")
			}
			stagedPath := filepath.Join(dir, filepath.FromSlash(file.Staged))
			info, statErr := os.Lstat(stagedPath)
			if statErr != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("staged transaction inode is missing")
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || uint64(stat.Dev) != file.AppliedDevice || stat.Ino != file.AppliedInode {
				return fmt.Errorf("staged transaction inode does not match journal")
			}
		}
	}
	if journal.Version == 1 && (len(journal.Preferences) > 0 || len(journal.Packages) > 0 || len(journal.System) > 0 || len(journal.Unresolved) > 0 || len(journal.RetainedApplications) > 0) {
		return fmt.Errorf("version 1 journal contains unsupported state")
	}
	if journal.Version == 2 {
		if err := validatePreferenceJournals(journal.Preferences); err != nil {
			return err
		}
		if err := validatePackageJournals(journal.Packages); err != nil {
			return err
		}
		if err := validateSystemJournals(journal.System); err != nil {
			return err
		}
		seenRetained := make(map[string]bool)
		for _, retained := range journal.RetainedApplications {
			if retained == "" || seenRetained[retained] {
				return fmt.Errorf("invalid retained application journal")
			}
			seenRetained[retained] = true
		}
	}
	return nil
}

func immediateJournalDotfile(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return !filepath.IsAbs(clean) && bundle.VettedDotfile(clean)
}

func validatePreferenceJournals(items []PreferenceJournal) error {
	allowed := make(map[string]bool)
	for _, spec := range preferenceSpecs(&models.MigrationState{}) {
		allowed[spec.Domain+":"+spec.Key] = true
	}
	// Empty state suppresses defaults, so enumerate the fixed writable keys.
	for _, key := range []string{"com.apple.finder:AppleShowAllFiles", "com.apple.finder:FXPreferredViewStyle", "com.apple.dock:autohide", "com.apple.dock:show-recents", "com.apple.dock:orientation", "-g:KeyRepeat", "-g:InitialKeyRepeat"} {
		allowed[key] = true
	}
	seen := make(map[string]bool)
	for _, item := range items {
		key := item.Domain + ":" + item.Key
		if !allowed[key] || seen[key] || !validPreferenceType(item.AppliedType) || item.AppliedValue == "" || item.Applied && item.Applying || item.RolledBack && item.RollingBack {
			return fmt.Errorf("invalid preference journal")
		}
		if item.Existed && !validPreferenceType(item.BeforeType) {
			return fmt.Errorf("invalid preference before state")
		}
		seen[key] = true
	}
	return nil
}

func validatePackageJournals(items []PackageJournal) error {
	seen := make(map[string]bool)
	for _, item := range items {
		key := item.Manager + ":" + item.Name
		valid := item.Manager == "homebrew" && (item.Type == "formula" || item.Type == "cask") && validPackageName(item.Name) || item.Manager == "vscode" && item.Type == "extension" && validExtensionID(item.Name) || item.Manager == "mas" && item.Type == "app-store" && regexp.MustCompile(`^[0-9]+$`).MatchString(item.Name)
		validLifecycle := !item.RollingBack && (item.Applying && !item.Applied && !item.RolledBack || !item.Applying && item.Applied || !item.Applying && !item.Applied && item.RolledBack)
		if !valid || seen[key] || !validLifecycle {
			return fmt.Errorf("invalid package journal")
		}
		seen[key] = true
	}
	return nil
}

func validateSystemJournals(items []SystemJournal) error {
	seen := make(map[string]bool)
	for _, item := range items {
		if (item.Kind != "computer-name" && item.Kind != "time-zone" && item.Kind != "language") || seen[item.Kind] || item.Before == "" || item.AppliedValue == "" || item.Applied && item.Applying || item.RolledBack && item.RollingBack {
			return fmt.Errorf("invalid system journal")
		}
		seen[item.Kind] = true
	}
	return nil
}

func validPreferenceType(kind string) bool {
	return kind == "bool" || kind == "int" || kind == "float" || kind == "string"
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fileState(path string) (string, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), info.Mode().Perm(), nil
}

func fileStateRegular(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("path is not a regular file: %s", path)
	}
	return fileState(path)
}

func fileIdentity(path string) (string, os.FileMode, uint64, uint64, error) {
	hash, mode, err := fileStateRegular(path)
	if err != nil {
		return "", 0, 0, 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, 0, 0, fmt.Errorf("cannot identify inode: %s", path)
	}
	return hash, mode, uint64(stat.Dev), stat.Ino, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".wave-transaction-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
