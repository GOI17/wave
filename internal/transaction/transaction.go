package transaction

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Version   int           `json:"version"`
	ID        string        `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	Status    string        `json:"status"`
	Files     []FileJournal `json:"files"`
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
	TransactionID string   `json:"transaction_id"`
	Restored      int      `json:"restored"`
	Removed       int      `json:"removed"`
	Conflicts     int      `json:"conflicts"`
	ConflictPaths []string `json:"conflict_paths"`
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
	applicationCount := len(state.Applications.Homebrew) + len(state.Applications.VSCodeExtensions)
	addPreviewCategory(result, "Applications", 0, applicationCount)
	addPreviewCategory(result, "Dotfiles", len(opened.Manifest.Files), 0)
	preferenceCount := countPreferences(state)
	addPreviewCategory(result, "Preferences", 0, preferenceCount)
	addPreviewCategory(result, "Environment", 0, 0)
	if applicationCount > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d application changes are preview-only and will not be applied", applicationCount))
	}
	if preferenceCount > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d preference changes are preview-only and will not be applied", preferenceCount))
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
	return rollbackUnlocked(latest.ID, homeDir, transactionsDir)
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
	summary.WriteString("Applications: preview-only\n")
	summary.WriteString("Preferences: preview-only\n")
	fmt.Fprintf(&summary, "\nRollback with: wave rollback --transaction %s --confirm\n", journal.ID)
	return summary.String()
}

// FormatRollbackSummary renders the canonical rollback completion summary.
func FormatRollbackSummary(result *RollbackResult) string {
	var summary strings.Builder
	summary.WriteString("Migration Rollback Summary\n")
	summary.WriteString("==========================\n")
	fmt.Fprintf(&summary, "Transaction: %s\n", result.TransactionID)
	fmt.Fprintf(&summary, "Files restored: %d\n", result.Restored)
	fmt.Fprintf(&summary, "Files removed: %d\n", result.Removed)
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
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return applyUnlocked(bundlePath, homeDir, transactionsDir)
}

func applyUnlocked(bundlePath, homeDir, transactionsDir string) (*Journal, error) {
	if err := recoverUnfinished(homeDir, transactionsDir); err != nil {
		return nil, err
	}
	opened, err := bundle.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	defer opened.Close()

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
	journal := &Journal{Version: 1, ID: id, CreatedAt: time.Now().UTC(), Status: "preparing", Files: []FileJournal{}}
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
			return recoverPartial(journal, homeDir, dir, err)
		}
		record.Applying = true
		if err := saveJournal(dir, journal); err != nil {
			return recoverPartial(journal, homeDir, dir, err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return recoverPartial(journal, homeDir, dir, err)
		}
		err = swapInFile(destination, *record, dir)
		if err != nil {
			return recoverPartial(journal, homeDir, dir, err)
		}
		record.Applied = true
		record.Applying = false
		if err := saveJournal(dir, journal); err != nil {
			return recoverPartial(journal, homeDir, dir, err)
		}
	}
	journal.Status = "applied"
	if err := saveJournal(dir, journal); err != nil {
		return journal, err
	}
	return journal, nil
}

// Rollback restores items that still match the state written by Apply.
func Rollback(id, homeDir, transactionsDir string) (*RollbackResult, error) {
	unlock, err := acquireLock(transactionsDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return rollbackUnlocked(id, homeDir, transactionsDir)
}

func rollbackUnlocked(id, homeDir, transactionsDir string) (*RollbackResult, error) {
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
	return rollbackJournal(journal, homeDir, dir)
}

func rollbackJournal(journal *Journal, homeDir, dir string) (*RollbackResult, error) {
	result := &RollbackResult{TransactionID: journal.ID, ConflictPaths: []string{}}
	for i := len(journal.Files) - 1; i >= 0; i-- {
		record := &journal.Files[i]
		if record.RolledBack {
			continue
		}
		destination, err := destinationPath(homeDir, record.Destination)
		if err != nil {
			return result, err
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
				return result, err
			}
			if err := syncDirectory(filepath.Dir(destination)); err != nil {
				return result, err
			}
			if err := syncDirectory(filepath.Dir(preserved)); err != nil {
				return result, err
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
				return result, err
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

func recoverPartial(journal *Journal, homeDir, dir string, applyErr error) (*Journal, error) {
	journal.Status = "partial"
	_ = saveJournal(dir, journal)
	result, rollbackErr := rollbackJournal(journal, homeDir, dir)
	if rollbackErr != nil || result.Conflicts > 0 {
		return journal, fmt.Errorf("apply failed: %w; automatic rollback incomplete", applyErr)
	}
	return journal, fmt.Errorf("apply failed and was rolled back: %w", applyErr)
}

func recoverUnfinished(homeDir, transactionsDir string) error {
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
		result, err := rollbackJournal(journal, homeDir, dir)
		if err != nil || result.Conflicts > 0 {
			return fmt.Errorf("unfinished transaction %s requires rollback resolution", journal.ID)
		}
	}
	return nil
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
	if journal.Version != 1 {
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
	return nil
}

func immediateJournalDotfile(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return !filepath.IsAbs(clean) && bundle.VettedDotfile(clean)
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
