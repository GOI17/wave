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
	"strings"

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

	manifest := Manifest{FormatVersion: formatVersion, State: state, Files: []File{}}
	writer := zip.NewWriter(tempFile)
	for _, entry := range state.Dotfiles.Files {
		file, data, ok := collectFile(homeDir, entry.Source)
		if !ok {
			manifest.Excluded++
			continue
		}
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
		manifest.Files = append(manifest.Files, file)
	}

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
	if err != nil || unsafePath(relative) || sensitivePath(relative) {
		return File{}, nil, false
	}
	resolvedHome, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		return File{}, nil, false
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return File{}, nil, false
	}
	resolvedRelative, err := filepath.Rel(resolvedHome, resolvedSource)
	if err != nil || unsafePath(resolvedRelative) {
		return File{}, nil, false
	}
	info, err := os.Stat(resolvedSource)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return File{}, nil, false
	}
	data, err := os.ReadFile(resolvedSource)
	if err != nil {
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

func unsafePath(path string) bool {
	clean := filepath.Clean(path)
	return filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func sensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasPrefix(lower, ".ssh/") || strings.HasPrefix(lower, ".gnupg/") ||
		strings.HasPrefix(lower, ".config/gcloud/") || strings.HasPrefix(lower, ".config/gh/") {
		return true
	}
	base := strings.ToLower(filepath.Base(lower))
	for _, marker := range []string{"secret", "token", "credential", "private_key", "id_rsa", "id_ed25519"} {
		if strings.Contains(base, marker) {
			return true
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
	err = json.NewDecoder(io.LimitReader(manifestReader, 4<<20)).Decode(&bundle.Manifest)
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
		if unsafePath(filepath.FromSlash(file.Destination)) || !strings.HasPrefix(file.Payload, "files/") || bundle.entries[file.Payload] == nil {
			_ = reader.Close()
			return nil, fmt.Errorf("invalid bundle file entry")
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
	return bundle, nil
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
	return data, nil
}

// Close closes the archive.
func (b *Bundle) Close() error {
	return b.reader.Close()
}
