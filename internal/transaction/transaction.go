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
	"time"

	"wave/internal/bundle"
)

const journalName = "journal.json"

// Journal records the durable state needed to rollback one apply.
type Journal struct {
	ID        string        `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	Status    string        `json:"status"`
	Files     []FileJournal `json:"files"`
}

// FileJournal records one applied file's before and after state.
type FileJournal struct {
	Destination string      `json:"destination"`
	Existed     bool        `json:"existed"`
	Before      string      `json:"before,omitempty"`
	BeforeMode  os.FileMode `json:"before_mode,omitempty"`
	AppliedHash string      `json:"applied_hash"`
	AppliedMode os.FileMode `json:"applied_mode"`
	RolledBack  bool        `json:"rolled_back"`
}

// RollbackResult summarizes a conflict-safe rollback.
type RollbackResult struct {
	TransactionID string   `json:"transaction_id"`
	Restored      int      `json:"restored"`
	Removed       int      `json:"removed"`
	Conflicts     int      `json:"conflicts"`
	ConflictPaths []string `json:"conflict_paths"`
}

// Apply atomically applies file payloads and persists a write-ahead journal.
func Apply(bundlePath, homeDir, transactionsDir string) (*Journal, error) {
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
	journal := &Journal{ID: id, CreatedAt: time.Now().UTC(), Status: "preparing", Files: []FileJournal{}}
	if err := saveJournal(dir, journal); err != nil {
		return nil, err
	}

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
		}
		if info, err := os.Stat(destination); err == nil {
			if !info.Mode().IsRegular() {
				return journal, fmt.Errorf("destination is not a regular file: %s", destination)
			}
			record.Existed = true
			record.BeforeMode = info.Mode().Perm()
			record.Before = filepath.ToSlash(filepath.Join("before", fmt.Sprintf("%06d", i)))
			beforeData, err := os.ReadFile(destination)
			if err != nil {
				return journal, err
			}
			if err := writeAtomic(filepath.Join(dir, filepath.FromSlash(record.Before)), beforeData, 0600); err != nil {
				return journal, err
			}
		} else if !os.IsNotExist(err) {
			return journal, err
		}
		journal.Files = append(journal.Files, record)
		if err := saveJournal(dir, journal); err != nil {
			return journal, err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return journal, err
		}
		if err := writeAtomic(destination, data, file.Mode); err != nil {
			journal.Status = "partial"
			_ = saveJournal(dir, journal)
			_, _ = rollbackJournal(journal, homeDir, dir)
			return journal, err
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
	if id == "" || filepath.Base(id) != id {
		return nil, fmt.Errorf("invalid transaction id")
	}
	dir := filepath.Join(transactionsDir, id)
	journal, err := loadJournal(dir)
	if err != nil {
		return nil, err
	}
	if journal.Status != "applied" && journal.Status != "rollback-conflicts" && journal.Status != "partial" {
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
		currentHash, currentMode, err := fileState(destination)
		if err != nil || currentHash != record.AppliedHash || currentMode != record.AppliedMode {
			result.Conflicts++
			result.ConflictPaths = append(result.ConflictPaths, record.Destination)
			continue
		}
		if !record.Existed {
			if err := os.Remove(destination); err != nil {
				return result, err
			}
			result.Removed++
		} else {
			beforeData, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(record.Before)))
			if err != nil {
				return result, err
			}
			if err := writeAtomic(destination, beforeData, record.BeforeMode); err != nil {
				return result, err
			}
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

func transactionID() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
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
	if err := json.Unmarshal(data, journal); err != nil {
		return nil, err
	}
	return journal, nil
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
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
