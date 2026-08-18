package cli

import "testing"

func TestGUICommandStartsServer(t *testing.T) {
	originalStartGUI := startGUI
	defer func() { startGUI = originalStartGUI }()

	var startedPort string
	startGUI = func(port string) error {
		startedPort = port
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
