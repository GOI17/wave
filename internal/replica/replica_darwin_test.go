//go:build darwin

package replica

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func testLocations(t *testing.T) Locations {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, apps := filepath.Join(root, "home"), filepath.Join(root, "Applications")
	for _, path := range []string{home, apps} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return Locations{Home: home, Applications: apps}
}

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0640); err != nil {
		t.Fatal(err)
	}
}

func TestReplicaRoundTripDocumentsAndApplicationFrameworkLinks(t *testing.T) {
	source, target := testLocations(t), testLocations(t)
	document := filepath.Join(source.Home, "Documents", "a file.txt")
	put(t, document, "private example\n")
	if err := unix.Setxattr(document, "com.wave.test", []byte("attribute data"), 0); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read,readattr,readextattr,readsecurity", document).CombinedOutput(); err != nil {
		t.Fatalf("set fixture ACL: %v: %s", err, output)
	}
	app := filepath.Join(source.Applications, "Example.app")
	put(t, filepath.Join(app, "Contents", "Versions", "A", "binary"), "example app")
	if err := os.Symlink("A", filepath.Join(app, "Contents", "Versions", "Current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(app, "Contents", "Versions", "A", "binary"), filepath.Join(app, "Contents", "duplicate")); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	selected := []Selection{{Root: "home", Path: "Documents/a file.txt"}, {Root: "applications", Path: "Example.app"}}
	if err := Capture(snapshot, selected, source); err != nil {
		t.Fatal(err)
	}
	if planned, err := Apply(snapshot, target, true); err != nil || len(planned) != 2 {
		t.Fatalf("plan=%v err=%v", planned, err)
	}
	if _, err := os.Stat(filepath.Join(target.Home, "Documents")); !os.IsNotExist(err) {
		t.Fatal("preview wrote destination")
	}
	if applied, err := Apply(snapshot, target, false); err != nil || len(applied) != 2 {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	result, err := Verify(snapshot, target)
	if err != nil || result.Matched != 2 || len(result.Differences) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if applied, err := Apply(snapshot, target, false); err != nil || len(applied) != 0 {
		t.Fatalf("reapply=%v err=%v", applied, err)
	}
	if err := unix.Setxattr(filepath.Join(target.Home, "Documents", "a file.txt"), "com.wave.test", []byte("changed attribute"), 0); err != nil {
		t.Fatal(err)
	}
	result, err = Verify(snapshot, target)
	if err != nil || len(result.Differences) != 1 {
		t.Fatalf("attribute change was not detected: result=%+v err=%v", result, err)
	}
	put(t, filepath.Join(target.Home, "Documents", "a file.txt"), "changed later")
	result, err = Verify(snapshot, target)
	if err != nil || len(result.Differences) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := Apply(snapshot, target, false); err == nil {
		t.Fatal("overwrote a conflicting document")
	}
	data, _ := os.ReadFile(filepath.Join(target.Home, "Documents", "a file.txt"))
	if string(data) != "changed later" {
		t.Fatal("conflict was overwritten")
	}
}

func TestReplicaAtomicPublishDoesNotReplaceExistingData(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	from, to := filepath.Join(root, "from"), filepath.Join(root, "to")
	put(t, from, "new")
	put(t, to, "existing")
	if err := renameExclusive(from, to); err == nil {
		t.Fatal("exclusive publish replaced existing destination")
	}
	data, _ := os.ReadFile(to)
	if string(data) != "existing" {
		t.Fatal("existing destination changed")
	}
}

func TestReplicaSurvivesMetadataPreservingTransport(t *testing.T) {
	source, target := testLocations(t), testLocations(t)
	file := filepath.Join(source.Home, "Documents", "transport.txt")
	put(t, file, "transport me")
	if err := unix.Setxattr(file, "com.wave.transport", []byte("metadata"), 0); err != nil {
		t.Fatal(err)
	}
	// Exercise a non-primary group when the test account belongs to one.
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group != os.Getgid() {
			if err := os.Chown(file, os.Getuid(), group); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(root, "snapshot")
	app := filepath.Join(source.Applications, "Transport.app")
	put(t, filepath.Join(app, "Contents", "Resources", "data"), "app data")
	if err := unix.Setxattr(app, "com.wave.transport", []byte("directory metadata"), 0); err != nil {
		t.Fatal(err)
	}
	if err := Capture(snapshot, []Selection{{Root: "home", Path: "Documents/transport.txt"}, {Root: "applications", Path: "Transport.app"}}, source); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "snapshot.tar")
	if out, err := exec.Command("/usr/bin/tar", "--format=pax", "--acls", "--xattrs", "--fflags", "-cpf", archive, "-C", root, "snapshot").CombinedOutput(); err != nil {
		t.Fatalf("export: %v: %s", err, out)
	}
	imported, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/tar", "--same-owner", "--mac-metadata", "--acls", "--xattrs", "--fflags", "-xpf", archive, "-C", imported).CombinedOutput(); err != nil {
		t.Fatalf("import: %v: %s", err, out)
	}
	if _, err := Apply(filepath.Join(imported, "snapshot"), target, false); err != nil {
		t.Fatal(err)
	}
	result, err := Verify(filepath.Join(imported, "snapshot"), target)
	if err != nil || result.Matched != 2 || len(result.Differences) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReplicaRejectsTraversalAndOverlappingSelections(t *testing.T) {
	locations := testLocations(t)
	for _, selection := range []Selection{{Root: "home", Path: "../outside"}, {Root: "home", Path: "Documents/../../outside"}, {Root: "home", Path: ".ssh/id_ed25519"}, {Root: "applications", Path: "../../bad.app"}, {Root: "other", Path: "Documents/x"}} {
		if _, err := location(selection, locations); err == nil {
			t.Fatalf("accepted %+v", selection)
		}
	}
	if err := validateSelections([]Selection{{Root: "home", Path: "Documents/folder"}, {Root: "home", Path: "Documents/folder/file"}}, locations); err == nil {
		t.Fatal("accepted overlapping selections")
	}
}

func TestReplicaRejectsEscapingLinksAndSymlinkedParents(t *testing.T) {
	source, target := testLocations(t), testLocations(t)
	app := filepath.Join(source.Applications, "Example.app")
	if err := os.Mkdir(app, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(app, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := Capture(filepath.Join(t.TempDir(), "snapshot"), []Selection{{Root: "applications", Path: "Example.app"}}, source); err == nil {
		t.Fatal("accepted escaping symlink")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(target.Home, "Documents")); err != nil {
		t.Fatal(err)
	}
	if _, err := location(Selection{Root: "home", Path: "Documents/file"}, target); err == nil {
		t.Fatal("accepted symlinked destination parent")
	}
}

func TestReplicaTamperFailsBeforeAnyWrites(t *testing.T) {
	source, target := testLocations(t), testLocations(t)
	put(t, filepath.Join(source.Home, "Documents", "first.txt"), "one")
	put(t, filepath.Join(source.Home, "Documents", "second.txt"), "two")
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err := Capture(snapshot, []Selection{{Root: "home", Path: "Documents/first.txt"}, {Root: "home", Path: "Documents/second.txt"}}, source); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(snapshot, "payload", "1"), "tampered")
	if _, err := Apply(snapshot, target, false); err == nil {
		t.Fatal("accepted corrupted payload")
	}
	if _, err := os.Stat(filepath.Join(target.Home, "Documents")); !os.IsNotExist(err) {
		t.Fatal("wrote before validating all payloads")
	}
}

func TestReplicaRejectsManifestDestinationEscape(t *testing.T) {
	source, target := testLocations(t), testLocations(t)
	put(t, filepath.Join(source.Home, "Documents", "file.txt"), "one")
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err := Capture(snapshot, []Selection{{Root: "home", Path: "Documents/file.txt"}}, source); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snapshot, "manifest.json")
	data, _ := os.ReadFile(path)
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Items[0].Path = "Documents/../../escaped"
	data, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(snapshot, target, false); err == nil {
		t.Fatal("accepted manifest path traversal")
	}
}
