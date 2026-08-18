package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/models"
)

func TestCopyDotfileRejectsSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	err := executor.copyDotfile(models.DotfileEntry{Source: path, Destination: path}, false)
	if err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("error = %v, want same-file error", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep me" {
		t.Fatalf("source was modified: %q", data)
	}
}

func TestCopyDotfileRejectsSymlinkToSameFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	err := executor.copyDotfile(models.DotfileEntry{Source: source, Destination: destination}, false)
	if err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("error = %v, want same-file error", err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep me" {
		t.Fatalf("source was modified: %q", data)
	}
}

func TestCopyDotfileCreatesIndependentBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	if err := executor.copyDotfile(models.DotfileEntry{Source: source, Destination: destination}, false); err != nil {
		t.Fatal(err)
	}

	backup := destination + ".backup"
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("backup = %q, want old content", data)
	}
	backupInfo, _ := os.Stat(backup)
	destinationInfo, _ := os.Stat(destination)
	if os.SameFile(backupInfo, destinationInfo) {
		t.Fatal("backup and destination share the same inode")
	}
}

func TestExecuteDotfilesReturnsCopyFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	_, err := executor.ExecuteDotfiles(&models.DotfilesGroup{Files: []models.DotfileEntry{{Source: path, Destination: path}}}, false)
	if err == nil {
		t.Fatal("ExecuteDotfiles returned nil error for a failed copy")
	}
}

func TestValidateStateRejectsLiveSelfCopyBeforeExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{dryRun: false}
	err := executor.ValidateState(&models.MigrationState{
		Version: "1.0.0",
		Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
			Source:      path,
			Destination: path,
		}}},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "refusing to apply") {
		t.Fatalf("error = %v, want live self-copy rejection", err)
	}
}

func TestValidateStateAllowsDryRunSelfCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{dryRun: true}
	err := executor.ValidateState(&models.MigrationState{
		Version: "1.0.0",
		Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{
			Source:      path,
			Destination: path,
		}}},
	}, true)
	if err != nil {
		t.Fatalf("dry-run validation error = %v", err)
	}
}

func TestCopyDotfileRejectsHardLinkedBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	backup := destination + ".backup"
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(destination, backup); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	err := executor.copyDotfile(models.DotfileEntry{Source: source, Destination: destination}, false)
	if err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("error = %v, want hard-linked backup error", err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("destination was modified: %q", data)
	}
}

func TestExecuteEnvironmentRejectsHardLinkedBackup(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(profile, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(profile, profile+".backup"); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{homeDir: dir}
	_, err := executor.ExecuteEnvironment(&models.EnvironmentGroup{
		ShellProfile: profile,
		Aliases:      map[string]string{"ll": "ls -la"},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("error = %v, want hard-linked backup error", err)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("profile was modified: %q", data)
	}
}

func TestExecuteDotfilesDryRunReportsMissingSourceWithoutFailing(t *testing.T) {
	executor := &macOSExecutor{}
	tasks, err := executor.ExecuteDotfiles(&models.DotfilesGroup{Files: []models.DotfileEntry{{
		Source:      filepath.Join(t.TempDir(), "missing"),
		Destination: filepath.Join(t.TempDir(), "destination"),
	}}}, true)
	if err != nil {
		t.Fatalf("dry-run returned error for unavailable source: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != "skipped" {
		t.Fatalf("tasks = %#v, want one skipped task", tasks)
	}
	if !strings.Contains(tasks[0].Error, "source unavailable") {
		t.Fatalf("task error = %q, want source unavailable warning", tasks[0].Error)
	}
}

func TestExecuteEnvironmentEscapesShellValues(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(profile, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{homeDir: dir}
	_, err := executor.ExecuteEnvironment(&models.EnvironmentGroup{
		ShellProfile: profile,
		Aliases:      map[string]string{"danger": "echo 'quoted'\necho injected"},
		EnvironmentVars: map[string]string{
			"SAFE_VALUE": "$(touch /tmp/injected)",
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `alias danger='echo '"'"'quoted'"'"'`+"\n"+`echo injected'`) {
		t.Fatalf("alias was not safely quoted: %q", content)
	}
	if !strings.Contains(content, `export SAFE_VALUE='$(touch /tmp/injected)'`) {
		t.Fatalf("environment value was not safely quoted: %q", content)
	}
}

func TestExecuteEnvironmentRejectsPathOutsideHome(t *testing.T) {
	executor := &macOSExecutor{homeDir: t.TempDir()}
	_, err := executor.ExecuteEnvironment(&models.EnvironmentGroup{
		ShellProfile: filepath.Join(t.TempDir(), ".zshrc"),
	}, false)
	if err == nil || !strings.Contains(err.Error(), "inside the home directory") {
		t.Fatalf("error = %v, want shell profile path rejection", err)
	}
}

func TestExecuteEnvironmentRejectsSymlinkOutsideHome(t *testing.T) {
	homeDir := t.TempDir()
	outsideDir := t.TempDir()
	link := filepath.Join(homeDir, "outside")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{homeDir: homeDir}
	_, err := executor.ExecuteEnvironment(&models.EnvironmentGroup{
		ShellProfile: filepath.Join(link, ".zshrc"),
	}, false)
	if err == nil || !strings.Contains(err.Error(), "inside the home directory") {
		t.Fatalf("error = %v, want symlink path rejection", err)
	}
}

func TestExecuteEnvironmentRejectsInvalidVariableName(t *testing.T) {
	dir := t.TempDir()
	executor := &macOSExecutor{homeDir: dir}
	_, err := executor.ExecuteEnvironment(&models.EnvironmentGroup{
		ShellProfile:    filepath.Join(dir, ".zshrc"),
		EnvironmentVars: map[string]string{"BAD; touch /tmp/injected": "value"},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "invalid environment variable name") {
		t.Fatalf("error = %v, want invalid variable rejection", err)
	}
}

func TestCopyDotfileRefreshesExistingBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	backup := destination + ".backup"
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	executor := &macOSExecutor{}
	if err := executor.copyDotfile(models.DotfileEntry{Source: source, Destination: destination}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "current" {
		t.Fatalf("backup = %q, want current destination content", data)
	}
}
