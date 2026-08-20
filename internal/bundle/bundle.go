package bundle

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"wave/internal/models"
)

const (
	manifestName  = "manifest.json"
	formatVersion = 1
	maxFileSize   = 32 << 20
	maxBundleSize = 512 << 20
	maxTotalSize  = 256 << 20
	maxFileCount  = 10000
)

var credentialAssignment = regexp.MustCompile(`(?i)["']?[A-Za-z0-9_./-]*(token|secret|password|credential|private[_-]?key|api[_-]?key|auth)[A-Za-z0-9_./-]*["']?\s*[:=]`)
var credentialURL = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*:)?//[^\s/:]+:[^\s/@]+@`)
var credentialTokenURL = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*:)?//[^\s/@]+@`)
var netrcCredential = regexp.MustCompile(`(?is)\bmachine\s+\S+.*\b(?:login|password|account)\s+\S+`)
var knownTokenValue = regexp.MustCompile(`(?i)(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|sk-(?:proj-)?[A-Za-z0-9_-]{20,}|sk_(?:live|test)_[A-Za-z0-9]{20,})`)
var vettedDotfiles = map[string]bool{
	".zshrc": true, ".bashrc": true, ".bash_profile": true, ".zsh_profile": true,
	".gitconfig": true, ".vimrc": true, ".tmux.conf": true, ".editorconfig": true,
	".prettierrc": true, ".eslintrc.json": true, ".gitignore_global": true,
}
var packageName = regexp.MustCompile(`^[A-Za-z0-9@+_.][A-Za-z0-9@+_.-]*(?:/[A-Za-z0-9@+_.-]+)*$`)
var extensionID = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]+$`)
var appStoreID = regexp.MustCompile(`^[0-9]+$`)
var timeZone = regexp.MustCompile(`^[A-Za-z0-9_+.-]+(?:/[A-Za-z0-9_+.-]+)*$`)
var language = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]+)*(?:,[A-Za-z]{2,3}(?:-[A-Za-z0-9]+)*)*$`)

// Manifest describes the portable contents of a .wave archive.
type Manifest struct {
	FormatVersion int                    `json:"format_version"`
	State         *models.MigrationState `json:"state"`
	Files         []File                 `json:"files"`
	Excluded      int                    `json:"excluded"`
}

// File describes one verified archive payload.
type File struct {
	Destination string      `json:"destination"`
	Payload     string      `json:"payload"`
	SHA256      string      `json:"sha256"`
	Mode        os.FileMode `json:"mode"`
	Size        int64       `json:"size"`
}

// Bundle is an opened, verified .wave archive.
type Bundle struct {
	Manifest Manifest
	reader   *zip.ReadCloser
	entries  map[string]*zip.File
}

// Inventory is the interface-neutral migration plan contained in an archive.
type Inventory struct {
	Groups []InventoryGroup `json:"groups"`
}

type InventoryGroup struct {
	Name           string   `json:"name"`
	WillMigrate    []string `json:"will_migrate"`
	WillNotMigrate []string `json:"will_not_migrate"`
}

// Create writes a portable migration archive with mode 0600.
func Create(path, homeDir string, state *models.MigrationState) error {
	if state == nil {
		return fmt.Errorf("migration state is nil")
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".wave-bundle-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := tempFile.Chmod(0600); err != nil {
		_ = tempFile.Close()
		return err
	}

	portableState := *state
	portableState.Dotfiles = models.DotfilesGroup{Files: []models.DotfileEntry{}, Directories: []models.DirEntry{}}
	portableState.Environment = models.EnvironmentGroup{}
	manifest := Manifest{FormatVersion: formatVersion, State: &portableState, Files: []File{}}
	writer := zip.NewWriter(tempFile)
	writtenPayloads := make(map[string]bool)
	for _, entry := range state.Dotfiles.Files {
		file, data, ok := collectFile(homeDir, entry.Source)
		if !ok {
			manifest.Excluded++
			if relative, err := filepath.Rel(homeDir, entry.Source); err == nil && !unsafePath(relative) && !sensitivePath(relative) {
				portableState.Dotfiles.Files = append(portableState.Dotfiles.Files, models.DotfileEntry{Source: filepath.ToSlash(relative), Destination: filepath.ToSlash(relative)})
			}
			continue
		}
		if !writtenPayloads[file.Payload] {
			payload, err := writer.Create(file.Payload)
			if err != nil {
				_ = writer.Close()
				_ = tempFile.Close()
				return err
			}
			if _, err := payload.Write(data); err != nil {
				_ = writer.Close()
				_ = tempFile.Close()
				return err
			}
			writtenPayloads[file.Payload] = true
		}
		manifest.Files = append(manifest.Files, file)
	}

	manifest.State = &portableState
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = writer.Close()
		_ = tempFile.Close()
		return err
	}
	manifestWriter, err := writer.Create(manifestName)
	if err != nil {
		_ = writer.Close()
		_ = tempFile.Close()
		return err
	}
	if _, err := manifestWriter.Write(manifestData); err != nil {
		_ = writer.Close()
		_ = tempFile.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func collectFile(homeDir, source string) (File, []byte, bool) {
	relative, err := filepath.Rel(homeDir, source)
	if err != nil || unsafePath(relative) || !VettedDotfile(relative) || sensitivePath(relative) {
		return File{}, nil, false
	}
	homeFD, err := unix.Open(homeDir, unix.O_RDONLY, 0)
	if err != nil {
		return File{}, nil, false
	}
	defer unix.Close(homeFD)
	fd, err := unix.Openat(homeFD, filepath.Base(relative), unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return File{}, nil, false
	}
	fileHandle := os.NewFile(uintptr(fd), source)
	defer fileHandle.Close()
	info, err := fileHandle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return File{}, nil, false
	}
	data, err := io.ReadAll(io.LimitReader(fileHandle, maxFileSize+1))
	if err != nil {
		return File{}, nil, false
	}
	if sensitiveContent(data) {
		return File{}, nil, false
	}
	hash := sha256.Sum256(data)
	hashString := hex.EncodeToString(hash[:])
	return File{
		Destination: filepath.ToSlash(relative),
		Payload:     "files/" + hashString,
		SHA256:      hashString,
		Mode:        info.Mode().Perm(),
		Size:        info.Size(),
	}, data, true
}

func immediateDotfile(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == "." && strings.HasPrefix(filepath.Base(clean), ".")
}

// VettedDotfile reports whether path is an explicitly supported root dotfile.
func VettedDotfile(path string) bool {
	clean := filepath.Clean(path)
	return immediateDotfile(clean) && vettedDotfiles[filepath.Base(clean)]
}

func unsafePath(path string) bool {
	clean := filepath.Clean(path)
	return filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func sensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasPrefix(filepath.Base(lower), ".wave-") {
		return true
	}
	if strings.HasPrefix(lower, ".ssh/") || strings.HasPrefix(lower, ".gnupg/") ||
		strings.HasPrefix(lower, ".config/gcloud/") || strings.HasPrefix(lower, ".config/gh/") {
		return true
	}
	base := strings.ToLower(filepath.Base(lower))
	if base == ".netrc" || base == "_netrc" || base == ".authinfo" || base == ".authinfo.gpg" || base == ".pgpass" {
		return true
	}
	for _, marker := range []string{"secret", "token", "credential", "private_key", "id_rsa", "id_ed25519"} {
		if strings.Contains(base, marker) {
			return true
		}
	}
	return false
}

func sensitiveContent(data []byte) bool {
	lower := strings.ToLower(string(data))
	if credentialURL.MatchString(lower) || credentialTokenURL.MatchString(lower) || netrcCredential.MatchString(lower) || knownTokenValue.MatchString(lower) {
		return true
	}
	if strings.Contains(lower, "-----begin ") && strings.Contains(lower, "private key-----") {
		return true
	}
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if credentialAssignment.MatchString(line) {
			return true
		}
		for _, marker := range []string{"api_key", "apikey", "api-token", "access_token", "auth_token", "authtoken", "client_secret", "password", "private_key", "github_token", "secret_access_key", "_authtoken"} {
			if strings.Contains(line, marker) && (strings.Contains(line, "=") || strings.Contains(line, ":")) {
				return true
			}
		}
	}
	return false
}

// Open validates and opens a portable migration archive.
func Open(path string) (*Bundle, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBundleSize {
		return nil, fmt.Errorf("bundle exceeds maximum size")
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{reader: reader, entries: make(map[string]*zip.File)}
	for _, entry := range reader.File {
		if strings.Contains(entry.Name, "\\") || unsafeArchivePath(entry.Name) {
			_ = reader.Close()
			return nil, fmt.Errorf("unsafe archive path: %s", entry.Name)
		}
		if _, exists := bundle.entries[entry.Name]; exists {
			_ = reader.Close()
			return nil, fmt.Errorf("duplicate archive entry: %s", entry.Name)
		}
		bundle.entries[entry.Name] = entry
	}
	manifestEntry := bundle.entries[manifestName]
	if manifestEntry == nil {
		_ = reader.Close()
		return nil, fmt.Errorf("bundle manifest is missing")
	}
	manifestReader, err := manifestEntry.Open()
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	decoder := json.NewDecoder(io.LimitReader(manifestReader, 4<<20))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&bundle.Manifest)
	_ = manifestReader.Close()
	if err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("decode bundle manifest: %w", err)
	}
	if bundle.Manifest.FormatVersion != formatVersion || bundle.Manifest.State == nil {
		_ = reader.Close()
		return nil, fmt.Errorf("unsupported bundle format")
	}
	if len(bundle.Manifest.Files) > maxFileCount {
		_ = reader.Close()
		return nil, fmt.Errorf("bundle contains too many files")
	}
	destinations := make(map[string]bool)
	var totalSize int64
	for _, file := range bundle.Manifest.Files {
		if unsafePath(filepath.FromSlash(file.Destination)) || !VettedDotfile(filepath.FromSlash(file.Destination)) || !strings.HasPrefix(file.Payload, "files/") || bundle.entries[file.Payload] == nil {
			_ = reader.Close()
			return nil, fmt.Errorf("invalid bundle file entry")
		}
		if file.Mode == 0 || file.Mode != file.Mode.Perm() || file.Mode.Perm() > 0777 {
			_ = reader.Close()
			return nil, fmt.Errorf("invalid file mode for %s", file.Destination)
		}
		if destinations[file.Destination] {
			_ = reader.Close()
			return nil, fmt.Errorf("duplicate destination: %s", file.Destination)
		}
		destinations[file.Destination] = true
		totalSize += file.Size
		if file.Size < 0 || totalSize > maxTotalSize {
			_ = reader.Close()
			return nil, fmt.Errorf("bundle payloads exceed maximum size")
		}
	}
	if err := validateMigrationState(bundle.Manifest.State); err != nil {
		_ = reader.Close()
		return nil, err
	}
	return bundle, nil
}

func validateMigrationState(state *models.MigrationState) error {
	for _, pkg := range state.Applications.Homebrew {
		if !packageName.MatchString(pkg.Name) || pkg.Type != "formula" && pkg.Type != "cask" {
			return fmt.Errorf("invalid Homebrew package: %s", pkg.Name)
		}
	}
	for _, extension := range state.Applications.VSCodeExtensions {
		if !extensionID.MatchString(extension) {
			return fmt.Errorf("invalid VS Code extension: %s", extension)
		}
	}
	for _, app := range state.Applications.AppStore {
		if !appStoreID.MatchString(app.BundleID) {
			return fmt.Errorf("invalid App Store ID: %s", app.BundleID)
		}
	}
	settings := state.Preferences.System
	if strings.ContainsAny(settings.ComputerName, "\r\n\x00") || len(settings.ComputerName) > 255 {
		return fmt.Errorf("invalid computer name")
	}
	if settings.TimeZone != "" && !timeZone.MatchString(settings.TimeZone) {
		return fmt.Errorf("invalid time zone")
	}
	if settings.Language != "" && !language.MatchString(settings.Language) {
		return fmt.Errorf("invalid language list")
	}
	return nil
}

func unsafeArchivePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../")
}

// ReadFile reads and verifies one file payload.
func (b *Bundle) ReadFile(file File) ([]byte, error) {
	entry := b.entries[file.Payload]
	if entry == nil || file.Size > maxFileSize || int64(entry.UncompressedSize64) != file.Size {
		return nil, fmt.Errorf("invalid payload size for %s", file.Destination)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != file.SHA256 {
		return nil, fmt.Errorf("checksum mismatch for %s", file.Destination)
	}
	if sensitivePath(filepath.FromSlash(file.Destination)) || sensitiveContent(data) {
		return nil, fmt.Errorf("sensitive payload rejected for %s", file.Destination)
	}
	return data, nil
}

// FormatSummary renders a human-readable inventory of a validated archive.
func FormatSummary(b *Bundle) string {
	return FormatInventory(BuildInventory(b))
}

// BuildInventory groups archive contents for CLI, TUI, and GUI rendering.
func BuildInventory(b *Bundle) Inventory {
	applications := InventoryGroup{Name: "Applications", WillMigrate: []string{}, WillNotMigrate: []string{}}
	for _, application := range b.Manifest.State.Applications.Homebrew {
		applications.WillMigrate = append(applications.WillMigrate, fmt.Sprintf("Homebrew %s: %s", application.Type, application.Name))
	}
	for _, extension := range b.Manifest.State.Applications.VSCodeExtensions {
		applications.WillMigrate = append(applications.WillMigrate, "VS Code extension: "+extension)
	}
	for _, application := range b.Manifest.State.Applications.AppStore {
		applications.WillMigrate = append(applications.WillMigrate, fmt.Sprintf("App Store: %s (%s)", application.Name, application.BundleID))
	}
	for _, application := range b.Manifest.State.Applications.Manual {
		applications.WillNotMigrate = append(applications.WillNotMigrate, fmt.Sprintf("%s (%s): no portable installer", application.Name, application.Path))
	}

	dotfiles := InventoryGroup{Name: "Dotfiles", WillMigrate: []string{}, WillNotMigrate: []string{}}
	for _, file := range b.Manifest.Files {
		dotfiles.WillMigrate = append(dotfiles.WillMigrate, fmt.Sprintf("%s (%d bytes, mode %04o)", file.Destination, file.Size, file.Mode.Perm()))
	}
	for _, file := range b.Manifest.State.Dotfiles.Files {
		dotfiles.WillNotMigrate = append(dotfiles.WillNotMigrate, file.Source+": unsafe, sensitive, nested, unavailable, or oversized")
	}

	settings := InventoryGroup{Name: "Settings", WillMigrate: migrationSettings(b.Manifest.State.Preferences), WillNotMigrate: []string{}}
	for _, setting := range unsupportedSettings(b.Manifest.State.Preferences) {
		settings.WillNotMigrate = append(settings.WillNotMigrate, setting+": unsupported")
	}
	return Inventory{Groups: []InventoryGroup{applications, dotfiles, settings}}
}

// FormatInventory renders the grouped inventory for plain-text interfaces.
func FormatInventory(inventory Inventory) string {
	var summary strings.Builder
	summary.WriteString("Captured Migration Summary\n")
	summary.WriteString("==========================\n")
	for _, group := range inventory.Groups {
		fmt.Fprintf(&summary, "\n%s\nWill migrate:\n", group.Name)
		writeInventoryItems(&summary, group.WillMigrate)
		summary.WriteString("Will not migrate:\n")
		writeInventoryItems(&summary, group.WillNotMigrate)
	}
	return summary.String()
}

func writeInventoryItems(summary *strings.Builder, items []string) {
	if len(items) == 0 {
		summary.WriteString("- none\n")
		return
	}
	for _, item := range items {
		fmt.Fprintf(summary, "- %s\n", item)
	}
}

func migrationSettings(prefs models.PreferencesGroup) []string {
	if preferencesEmpty(prefs) {
		return nil
	}
	settings := []string{
		fmt.Sprintf("Finder show hidden files = %t", prefs.Finder.ShowHiddenFiles),
		fmt.Sprintf("Dock autohide = %t", prefs.Dock.Autohide),
		fmt.Sprintf("Dock show recents = %t", prefs.Dock.ShowRecents),
	}
	for name, value := range map[string]any{
		"Finder default view": prefs.Finder.DefaultViewMode, "Dock position": prefs.Dock.Position,
		"Keyboard repeat": prefs.Keyboard.KeyRepeat, "Keyboard initial repeat": prefs.Keyboard.InitialRepeat,
		"Computer name": prefs.System.ComputerName, "Time zone": prefs.System.TimeZone, "Languages": prefs.System.Language,
	} {
		if fmt.Sprint(value) != "" && fmt.Sprint(value) != "0" {
			settings = append(settings, fmt.Sprintf("%s = %v", name, value))
		}
	}
	sort.Strings(settings)
	return settings
}

func preferencesEmpty(prefs models.PreferencesGroup) bool {
	return prefs.Finder == (models.FinderPrefs{}) && prefs.Dock.Position == "" && !prefs.Dock.Autohide && !prefs.Dock.ShowRecents && len(prefs.Dock.AppOrder) == 0 && len(prefs.Dock.PersistentApps) == 0 && prefs.Keyboard == (models.KeyboardPrefs{}) && prefs.Trackpad == (models.TrackpadPrefs{}) && prefs.System == (models.SystemPrefs{}) && len(prefs.Apps) == 0
}

func unsupportedSettings(prefs models.PreferencesGroup) []string {
	var settings []string
	if prefs.Keyboard.NumLock {
		settings = append(settings, "Keyboard num lock")
	}
	if prefs.Trackpad != (models.TrackpadPrefs{}) {
		settings = append(settings, "Trackpad preferences")
	}
	if prefs.System.ScreenBrightness != 0 {
		settings = append(settings, "Screen brightness")
	}
	if len(prefs.Dock.AppOrder) > 0 || len(prefs.Dock.PersistentApps) > 0 {
		settings = append(settings, "Dock application layout")
	}
	if len(prefs.Apps) > 0 {
		settings = append(settings, "Application-specific preferences")
	}
	return settings
}

// Close closes the archive.
func (b *Bundle) Close() error {
	return b.reader.Close()
}
