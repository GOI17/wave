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
	"strconv"
	"strings"
	"time"

	"wave/internal/bundle"
	"wave/internal/models"
)

const journalName = "journal.json"

// Journal records the durable state needed to rollback one apply.
type Journal struct {
	ID          string              `json:"id"`
	CreatedAt   time.Time           `json:"created_at"`
	Status      string              `json:"status"`
	Files       []FileJournal       `json:"files"`
	Preferences []PreferenceJournal `json:"preferences"`
	Packages    []PackageJournal    `json:"packages"`
}

// PreferenceJournal records one defaults key's before and applied values.
type PreferenceJournal struct {
	Domain       string `json:"domain"`
	Key          string `json:"key"`
	Existed      bool   `json:"existed"`
	BeforeType   string `json:"before_type,omitempty"`
	BeforeValue  string `json:"before_value,omitempty"`
	AppliedType  string `json:"applied_type"`
	AppliedValue string `json:"applied_value"`
	RolledBack   bool   `json:"rolled_back"`
}

// PackageJournal records software introduced by one transaction.
type PackageJournal struct {
	Manager        string `json:"manager"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	AppliedVersion string `json:"applied_version,omitempty"`
	RolledBack     bool   `json:"rolled_back"`
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
	TransactionID       string   `json:"transaction_id"`
	Restored            int      `json:"restored"`
	Removed             int      `json:"removed"`
	Conflicts           int      `json:"conflicts"`
	ConflictPaths       []string `json:"conflict_paths"`
	PreferencesRestored int      `json:"preferences_restored"`
	PackagesRemoved     int      `json:"packages_removed"`
}

// System runs platform commands used by preference and package transactions.
type System interface {
	Output(name string, args ...string) (string, error)
	Run(name string, args ...string) error
}

type execSystem struct{}

func (execSystem) Output(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (execSystem) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// Apply atomically applies file payloads and persists a write-ahead journal.
func Apply(bundlePath, homeDir, transactionsDir string) (*Journal, error) {
	return ApplyWithSystem(bundlePath, homeDir, transactionsDir, execSystem{})
}

// ApplyWithSystem applies a bundle using the supplied platform command boundary.
func ApplyWithSystem(bundlePath, homeDir, transactionsDir string, system System) (*Journal, error) {
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
	journal := &Journal{ID: id, CreatedAt: time.Now().UTC(), Status: "preparing", Files: []FileJournal{}, Preferences: []PreferenceJournal{}, Packages: []PackageJournal{}}
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
			_, _ = rollbackJournal(journal, homeDir, dir, system)
			return journal, err
		}
	}
	if err := applyPreferences(journal, opened.Manifest.State, system, dir); err != nil {
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

// RollbackWithSystem rolls back a transaction using the supplied command boundary.
func RollbackWithSystem(id, homeDir, transactionsDir string, system System) (*RollbackResult, error) {
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
	return rollbackJournal(journal, homeDir, dir, system)
}

func rollbackJournal(journal *Journal, homeDir, dir string, system System) (*RollbackResult, error) {
	result := &RollbackResult{TransactionID: journal.ID, ConflictPaths: []string{}}
	for i := len(journal.Packages) - 1; i >= 0; i-- {
		record := &journal.Packages[i]
		if record.RolledBack {
			continue
		}
		version, installed := packageVersion(system, record.Manager, record.Name, record.Type)
		if !installed || (record.AppliedVersion != "" && version != record.AppliedVersion) {
			result.Conflicts++
			result.ConflictPaths = append(result.ConflictPaths, "package:"+record.Name)
			continue
		}
		if err := uninstallPackage(system, *record); err != nil {
			return result, err
		}
		record.RolledBack = true
		result.PackagesRemoved++
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
		if err != nil || !current.Existed || current.Type != record.AppliedType || current.Value != record.AppliedValue {
			result.Conflicts++
			result.ConflictPaths = append(result.ConflictPaths, "preference:"+record.Domain+":"+record.Key)
			continue
		}
		if record.Existed {
			if err := writePreference(system, record.Domain, record.Key, record.BeforeType, record.BeforeValue); err != nil {
				return result, err
			}
		} else if err := system.Run("defaults", "delete", record.Domain, record.Key); err != nil {
			return result, err
		}
		record.RolledBack = true
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

func recoverPartial(journal *Journal, homeDir, dir string, system System, applyErr error) (*Journal, error) {
	journal.Status = "partial"
	_ = saveJournal(dir, journal)
	result, rollbackErr := rollbackJournal(journal, homeDir, dir, system)
	if rollbackErr != nil || result.Conflicts > 0 {
		return journal, fmt.Errorf("apply failed: %w; automatic rollback incomplete", applyErr)
	}
	return journal, fmt.Errorf("apply failed and was rolled back: %w", applyErr)
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
		record := PreferenceJournal{Domain: spec.Domain, Key: spec.Key, Existed: before.Existed, BeforeType: before.Type, BeforeValue: before.Value, AppliedType: spec.Type, AppliedValue: spec.Value}
		journal.Preferences = append(journal.Preferences, record)
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
		if err := writePreference(system, spec.Domain, spec.Key, spec.Type, spec.Value); err != nil {
			return err
		}
	}
	return nil
}

func preferenceSpecs(state *models.MigrationState) []preferenceSpec {
	prefs := state.Preferences
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

func readPreference(system System, domain, key string) (preferenceValue, error) {
	value, err := system.Output("defaults", "read", domain, key)
	if err != nil {
		return preferenceValue{Existed: false}, nil
	}
	typeOutput, err := system.Output("defaults", "read-type", domain, key)
	if err != nil {
		return preferenceValue{}, err
	}
	return preferenceValue{Existed: true, Type: preferenceType(typeOutput), Value: normalizePreference(preferenceType(typeOutput), value)}, nil
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
	flag := "-" + kind
	return system.Run("defaults", "write", domain, key, flag, value)
}

func applyPackages(journal *Journal, state *models.MigrationState, system System, dir string) error {
	for _, pkg := range state.Applications.Homebrew {
		if _, installed := packageVersion(system, "homebrew", pkg.Name, pkg.Type); installed {
			continue
		}
		args := []string{"install"}
		if pkg.Type == "cask" {
			args = append(args, "--cask")
		}
		args = append(args, pkg.Name)
		if err := system.Run("brew", args...); err != nil {
			return err
		}
		version, _ := packageVersion(system, "homebrew", pkg.Name, pkg.Type)
		journal.Packages = append(journal.Packages, PackageJournal{Manager: "homebrew", Name: pkg.Name, Type: pkg.Type, AppliedVersion: version})
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	for _, extension := range state.Applications.VSCodeExtensions {
		if _, installed := packageVersion(system, "vscode", extension, "extension"); installed {
			continue
		}
		if err := system.Run("code", "--install-extension", extension); err != nil {
			return err
		}
		journal.Packages = append(journal.Packages, PackageJournal{Manager: "vscode", Name: extension, Type: "extension"})
		if err := saveJournal(dir, journal); err != nil {
			return err
		}
	}
	return nil
}

func packageVersion(system System, manager, name, kind string) (string, bool) {
	if manager == "vscode" {
		output, err := system.Output("code", "--list-extensions")
		if err != nil {
			return "", false
		}
		for _, extension := range strings.Split(output, "\n") {
			if strings.EqualFold(strings.TrimSpace(extension), name) {
				return "", true
			}
		}
		return "", false
	}
	args := []string{"list"}
	if kind == "cask" {
		args = append(args, "--cask")
	}
	args = append(args, "--versions", name)
	output, err := system.Output("brew", args...)
	if err != nil || strings.TrimSpace(output) == "" {
		return "", false
	}
	return strings.TrimSpace(output), true
}

func uninstallPackage(system System, pkg PackageJournal) error {
	if pkg.Manager == "vscode" {
		return system.Run("code", "--uninstall-extension", pkg.Name)
	}
	args := []string{"uninstall"}
	if pkg.Type == "cask" {
		args = append(args, "--cask")
	}
	return system.Run("brew", append(args, pkg.Name)...)
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
