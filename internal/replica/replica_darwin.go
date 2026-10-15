//go:build darwin

// Package replica handles explicitly selected documents and self-contained app
// bundles. It is separate from the conservative .wave dotfile format.
package replica

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Locations struct{ Home, Applications string }
type Selection struct {
	Root string `json:"root"`
	Path string `json:"path"`
}
type Entry struct {
	Path       string            `json:"path"`
	Type       string            `json:"type"`
	Mode       uint32            `json:"mode"`
	Size       int64             `json:"size,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Link       string            `json:"link,omitempty"`
	Hardlink   string            `json:"hardlink,omitempty"`
	UID        uint32            `json:"uid"`
	GID        uint32            `json:"gid"`
	Modified   int64             `json:"modified_ns"`
	Flags      uint32            `json:"flags,omitempty"`
	Attributes map[string]string `json:"attribute_hashes,omitempty"`
	ACL        string            `json:"acl,omitempty"`
}
type Item struct {
	Selection
	Entries []Entry `json:"entries"`
}
type Manifest struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}
type Result struct {
	Matched     int      `json:"matched"`
	Differences []string `json:"differences"`
}

func location(selection Selection, locations Locations) (string, error) {
	path := selection.Path
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return "", fmt.Errorf("invalid replica destination %q", path)
	}
	var root string
	switch selection.Root {
	case "applications":
		if filepath.Base(path) != path || !strings.HasSuffix(path, ".app") {
			return "", fmt.Errorf("application must be a single .app bundle name")
		}
		root = locations.Applications
	case "home":
		parts := strings.Split(path, string(filepath.Separator))
		if len(parts) < 2 {
			return "", fmt.Errorf("select a document below a personal folder")
		}
		switch parts[0] {
		case "Documents", "Desktop", "Downloads", "Pictures", "Movies", "Music":
		default:
			return "", fmt.Errorf("unsupported personal folder %q", parts[0])
		}
		root = locations.Home
	default:
		return "", fmt.Errorf("unknown replica root %q", selection.Root)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("replica root must be absolute")
	}
	destination := filepath.Join(root, path)
	// Refuse symlinked parents, including ancestors of the selected root.
	for parent := filepath.Dir(destination); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return "", fmt.Errorf("unsafe parent %s", parent)
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	return destination, nil
}

func validateSelections(items []Selection, locations Locations) error {
	if len(items) == 0 || len(items) > 1000 {
		return fmt.Errorf("select between 1 and 1000 items")
	}
	paths := []string{}
	for _, item := range items {
		path, err := location(item, locations)
		if err != nil {
			return err
		}
		for _, previous := range paths {
			if path == previous || strings.HasPrefix(path, previous+"/") || strings.HasPrefix(previous, path+"/") {
				return fmt.Errorf("duplicate or overlapping selections")
			}
		}
		paths = append(paths, path)
	}
	return nil
}

func copyNative(source, destination string) error {
	command := exec.Command("/usr/bin/ditto", "--rsrc", "--extattr", "--acl", source, destination)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("copy item: %w: %s", err, strings.TrimSpace(string(output)))
	}
	// copyfile/ditto creates symlinks with fresh timestamps. Restore the link's
	// own timestamps without following it; otherwise app frameworks differ.
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		times := []unix.Timespec{{Sec: stat.Atimespec.Sec, Nsec: stat.Atimespec.Nsec}, {Sec: stat.Mtimespec.Sec, Nsec: stat.Mtimespec.Nsec}}
		return unix.UtimesNanoAt(unix.AT_FDCWD, filepath.Join(destination, relative), times, unix.AT_SYMLINK_NOFOLLOW)
	})
}

// Capture creates a new private directory snapshot, with no overwrites. The
// before/copy/after comparison rejects source files changing during capture.
func Capture(output string, selections []Selection, locations Locations) error {
	if err := validateSelections(selections, locations); err != nil {
		return err
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return err
	}
	output = filepath.Join(parent, filepath.Base(output))
	for _, selection := range selections {
		source, _ := location(selection, locations)
		if output == source || strings.HasPrefix(output, source+"/") {
			return fmt.Errorf("snapshot cannot be stored inside a selected item")
		}
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return fmt.Errorf("snapshot destination already exists or is inaccessible")
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".wave-replica-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.Mkdir(filepath.Join(stage, "payload"), 0700); err != nil {
		return err
	}
	manifest := Manifest{Version: 1, Items: []Item{}}
	for index, selection := range selections {
		source, _ := location(selection, locations)
		if selection.Root == "applications" {
			info, err := os.Lstat(source)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("application must be a directory bundle: %s", selection.Path)
			}
		}
		before, err := inventory(source)
		if err != nil {
			return err
		}
		payload := filepath.Join(stage, "payload", fmt.Sprint(index))
		if err := copyNative(source, payload); err != nil {
			return err
		}
		copied, err := inventory(payload)
		if err != nil {
			return err
		}
		after, err := inventory(source)
		if err != nil {
			return err
		}
		if difference := firstDifference(before, copied); difference != "" {
			return fmt.Errorf("copy did not preserve %s: %s", selection.Path, difference)
		}
		if difference := firstDifference(before, after); difference != "" {
			return fmt.Errorf("source changed during capture %s: %s", selection.Path, difference)
		}
		manifest.Items = append(manifest.Items, Item{Selection: selection, Entries: copied})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), data, 0600); err != nil {
		return err
	}
	return renameExclusive(stage, output)
}

func readSnapshot(path string, locations Locations) (*Manifest, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("snapshot must be a directory")
	}
	manifestPath := filepath.Join(path, "manifest.json")
	info, err = os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, fmt.Errorf("invalid replica manifest")
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<20))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("trailing data after replica manifest")
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("unsupported replica version")
	}
	selections := make([]Selection, len(manifest.Items))
	for index, item := range manifest.Items {
		selections[index] = item.Selection
	}
	if err := validateSelections(selections, locations); err != nil {
		return nil, err
	}
	payloadInfo, err := os.Lstat(filepath.Join(path, "payload"))
	if err != nil || !payloadInfo.IsDir() {
		return nil, fmt.Errorf("invalid payload directory")
	}
	for index, item := range manifest.Items {
		actual, err := inventory(filepath.Join(path, "payload", fmt.Sprint(index)))
		if err != nil {
			return nil, err
		}
		if item.Root == "applications" && actual[0].Type != "directory" {
			return nil, fmt.Errorf("application payload must be a directory")
		}
		if difference := firstDifference(item.Entries, actual); difference != "" {
			return nil, fmt.Errorf("payload integrity mismatch %s: %s", item.Path, difference)
		}
	}
	return &manifest, nil
}

// Apply refuses different existing items and preflights the entire snapshot.
// It publishes each verified item atomically without replacing an existing path.
// A repeated apply skips identical items. It never deletes target data.
func Apply(path string, locations Locations, dryRun bool) ([]string, error) {
	manifest, err := readSnapshot(path, locations)
	if err != nil {
		return nil, err
	}
	pending := make(map[int]string)
	for index, item := range manifest.Items {
		destination, err := location(item.Selection, locations)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(destination); err == nil {
			actual, err := inventory(destination)
			if err != nil || !reflect.DeepEqual(actual, item.Entries) {
				return nil, fmt.Errorf("destination conflict: %s", destination)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		} else {
			pending[index] = destination
		}
	}
	applied := []string{}
	for index, item := range manifest.Items {
		destination, ok := pending[index]
		if !ok {
			continue
		}
		if dryRun {
			applied = append(applied, destination)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return applied, err
		}
		if _, err := location(item.Selection, locations); err != nil {
			return applied, err
		}
		stage, err := os.MkdirTemp(filepath.Dir(destination), ".wave-replica-")
		if err != nil {
			return applied, err
		}
		candidate := filepath.Join(stage, "item")
		err = copyNative(filepath.Join(path, "payload", fmt.Sprint(index)), candidate)
		if err == nil {
			var actual []Entry
			actual, err = inventory(candidate)
			if err == nil && !reflect.DeepEqual(actual, item.Entries) {
				err = fmt.Errorf("copied item failed verification: %s", item.Path)
			}
		}
		if err == nil {
			err = renameExclusive(candidate, destination)
		}
		os.RemoveAll(stage)
		if err != nil {
			return applied, err
		}
		applied = append(applied, destination)
	}
	return applied, nil
}

func Verify(path string, locations Locations) (*Result, error) {
	manifest, err := readSnapshot(path, locations)
	if err != nil {
		return nil, err
	}
	result := &Result{Differences: []string{}}
	for _, item := range manifest.Items {
		destination, err := location(item.Selection, locations)
		if err != nil {
			return nil, err
		}
		actual, err := inventory(destination)
		if err != nil || !reflect.DeepEqual(actual, item.Entries) {
			result.Differences = append(result.Differences, item.Root+":"+item.Path)
		} else {
			result.Matched++
		}
	}
	return result, nil
}

func inventory(root string) ([]Entry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() && !rootInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("selected root must be a regular file or directory")
	}
	entries := []Entry{}
	hardlinks := map[[2]uint64]string{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if len(entries) >= 100000 {
			return fmt.Errorf("item exceeds 100000 entries")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		entry := Entry{Path: relative, Mode: uint32(stat.Mode & 07777), UID: stat.Uid, GID: stat.Gid, Modified: info.ModTime().UnixNano(), Flags: stat.Flags}
		switch {
		case info.Mode().IsRegular():
			entry.Type, entry.Size = "file", info.Size()
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil {
				return err
			}
			entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
			if stat.Nlink > 1 {
				key := [2]uint64{uint64(stat.Dev), stat.Ino}
				if first, ok := hardlinks[key]; ok {
					entry.Hardlink = first
				} else {
					hardlinks[key] = relative
				}
			}
		case info.IsDir():
			entry.Type = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			entry.Type = "symlink"
			entry.Link, err = os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), entry.Link))
			if filepath.IsAbs(entry.Link) || (resolved != root && !strings.HasPrefix(resolved, root+"/")) {
				return fmt.Errorf("symlink escapes selected item: %s", relative)
			}
		default:
			return fmt.Errorf("unsupported special file: %s", relative)
		}
		entry.Attributes, err = attributes(path)
		if err != nil {
			return err
		}
		acl, err := exec.Command("/bin/ls", "-lde", path).Output()
		if err != nil {
			return fmt.Errorf("read ACL: %w", err)
		}
		_, entry.ACL, _ = strings.Cut(string(acl), "\n")
		entries = append(entries, entry)
		return nil
	})
	return entries, err
}

func firstDifference(expected, actual []Entry) string {
	if len(expected) != len(actual) {
		return fmt.Sprintf("entry count %d != %d", len(expected), len(actual))
	}
	for index, want := range expected {
		got := actual[index]
		if reflect.DeepEqual(want, got) {
			continue
		}
		fields := []string{}
		left, right := reflect.ValueOf(want), reflect.ValueOf(got)
		for field := 0; field < left.NumField(); field++ {
			if !reflect.DeepEqual(left.Field(field).Interface(), right.Field(field).Interface()) {
				fields = append(fields, left.Type().Field(field).Name)
			}
		}
		return want.Path + " (" + strings.Join(fields, ", ") + ")"
	}
	return ""
}

func attributes(path string) (map[string]string, error) {
	size, err := unix.Llistxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buffer := make([]byte, size)
	n, err := unix.Llistxattr(path, buffer)
	if err != nil {
		return nil, err
	}
	names := strings.Split(strings.TrimRight(string(buffer[:n]), "\x00"), "\x00")
	sort.Strings(names)
	values := map[string]string{}
	for _, name := range names {
		size, err := unix.Lgetxattr(path, name, nil)
		if err != nil {
			return nil, err
		}
		if size > 64<<20 {
			return nil, fmt.Errorf("extended attribute too large")
		}
		buffer := make([]byte, size)
		n, err := unix.Lgetxattr(path, name, buffer)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(buffer[:n])
		values[name] = hex.EncodeToString(hash[:])
	}
	return values, nil
}

func renameExclusive(source, destination string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	fd := int32(-2)
	_, _, errno := syscall.Syscall6(unix.SYS_RENAMEATX_NP, uintptr(fd), uintptr(unsafe.Pointer(from)), uintptr(fd), uintptr(unsafe.Pointer(to)), 0x4|0x10, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
