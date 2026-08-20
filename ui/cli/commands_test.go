package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestGUICommandStartsServer(t *testing.T) {
	originalStartGUI := startGUI
	defer func() { startGUI = originalStartGUI }()

	var startedPort string
	startGUI = func(port, version string) error {
		startedPort = port
		if version != Version {
			t.Fatalf("GUI version = %q, want %q", version, Version)
		}
		return nil
	}

	if err := guiCmd.Flags().Set("port", "4321"); err != nil {
		t.Fatal(err)
	}
	defer guiCmd.Flags().Set("port", "8080")

	if err := guiCmd.RunE(guiCmd, nil); err != nil {
		t.Fatalf("gui command error = %v", err)
	}
	if startedPort != "4321" {
		t.Fatalf("started port = %q, want 4321", startedPort)
	}
}

func TestUpdateUsesHomebrewFormula(t *testing.T) {
	original := runBrew
	defer func() { runBrew = original }()
	var arguments []string
	runBrew = func(args ...string) error {
		arguments = args
		return nil
	}
	if err := updateCmd.RunE(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"upgrade", "GOI17/wave/wave"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
	}
}

func TestUninstallRequiresConfirmation(t *testing.T) {
	original := uninstallConfirm
	uninstallConfirm = false
	defer func() { uninstallConfirm = original }()
	if err := uninstallCmd.RunE(uninstallCmd, nil); err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("error = %v, want confirmation error", err)
	}
}

func TestConfirmedUninstallUsesHomebrewFormula(t *testing.T) {
	originalRun := runBrew
	originalConfirm := uninstallConfirm
	defer func() {
		runBrew = originalRun
		uninstallConfirm = originalConfirm
	}()
	uninstallConfirm = true
	var arguments []string
	runBrew = func(args ...string) error {
		arguments = args
		return nil
	}
	if err := uninstallCmd.RunE(uninstallCmd, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"uninstall", "GOI17/wave/wave"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
	}
}

func TestRollbackRequiresConfirmation(t *testing.T) {
	original := rollbackConfirm
	rollbackConfirm = false
	defer func() { rollbackConfirm = original }()

	err := rollbackCmd.RunE(rollbackCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("error = %v, want confirmation error", err)
	}
}
