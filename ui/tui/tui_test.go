package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"wave/internal/bundle"
	"wave/internal/models"
)

func TestModelRunsSelectedAction(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		want   cmdType
	}{
		{name: "capture", cursor: 0, want: captureCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("1.0.1")
			model.cursor = tt.cursor
			model.run = func(cmd cmdType, _ string) tea.Cmd {
				if cmd != tt.want {
					t.Fatalf("run command = %q, want %q", cmd, tt.want)
				}
				return commandResult(cmd, nil)
			}

			updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if command == nil {
				t.Fatal("selection returned no command")
			}

			updated, _ = updated.(Model).Update(command())
			if !strings.Contains(updated.(Model).status, "completed") {
				t.Fatalf("status = %q, want completed status", updated.(Model).status)
			}
		})
	}
}

func TestModelConfirmsMutatingActions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cursor int
		want   cmdType
	}{{name: "rollback", cursor: 3, want: rollbackCmd}} {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("1.0.3")
			model.cursor = tt.cursor
			called := false
			model.run = func(cmd cmdType, _ string) tea.Cmd {
				called = true
				if cmd != tt.want {
					t.Fatalf("command = %q, want %q", cmd, tt.want)
				}
				return commandResult(cmd, nil)
			}
			updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if command != nil || called || updated.(Model).pending != tt.want {
				t.Fatalf("mutation ran before confirmation: %#v", updated)
			}
			_, command = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			if command == nil || !called {
				t.Fatal("confirmed mutation did not run")
			}
		})
	}
}

func TestPreviewIncludesArchiveInventory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	source := filepath.Join(homeDir, ".vimrc")
	if err := os.WriteFile(source, []byte("set number\n"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(homeDir, "selected.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: source}}}}
	if err := bundle.Create(archive, homeDir, state); err != nil {
		t.Fatal(err)
	}
	result := runPortableCommand(applyCmd, archive)().(cmdMsg)
	if result.err != nil || !strings.Contains(result.output, "Migration Preview Summary") || result.inventory == nil || len(result.inventory.Groups) != 3 || !strings.Contains(result.inventory.Groups[1].WillMigrate[0], ".vimrc") {
		t.Fatalf("preview result = %#v", result)
	}
}

func TestDiscoverArchivesIncludesCommonTransferDirectories(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	downloads := filepath.Join(homeDir, "Downloads")
	if err := os.MkdirAll(downloads, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(downloads, "received.wave")
	if err := os.WriteFile(archive, nil, 0600); err != nil {
		t.Fatal(err)
	}
	result := discoverArchives()().(archivesMsg)
	if result.err != nil || len(result.paths) != 1 || result.paths[0] != archive {
		t.Fatalf("archives = %#v", result)
	}
}

func TestModelShowsCommandError(t *testing.T) {
	model := InitialModel("1.0.1")
	model.run = func(cmd cmdType, _ string) tea.Cmd {
		return commandResult(cmd, errors.New("boom"))
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(Model).Update(command())
	if !strings.Contains(updated.(Model).status, "boom") {
		t.Fatalf("status = %q, want command error", updated.(Model).status)
	}
}

func TestViewUsesRuntimeVersionAndDarculaPalette(t *testing.T) {
	view := InitialModel("9.8.7").View()
	if !strings.Contains(view, "v9.8.7") {
		t.Fatalf("view = %q, want runtime version", view)
	}
	if titleStyle.GetForeground() != lipgloss.Color("#CC7832") {
		t.Fatalf("title foreground = %q, want Darcula orange", titleStyle.GetForeground())
	}
}

func TestModelUsesTerminalDimensions(t *testing.T) {
	model := InitialModel("1.0.3")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	resized := updated.(Model)
	if resized.width != 120 || resized.height != 40 {
		t.Fatalf("dimensions = %dx%d, want 120x40", resized.width, resized.height)
	}
	if rendered := resized.View(); lipgloss.Width(rendered) != 120 {
		t.Fatalf("rendered width = %d, want 120", lipgloss.Width(rendered))
	}
}

func TestCommandFor(t *testing.T) {
	tests := []struct {
		name string
		cmd  cmdType
		want []string
	}{
		{name: "capture", cmd: captureCmd, want: []string{"/tmp/wave", "capture", "--output", "/tmp/state.yaml"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := commandFor(tt.cmd, "/tmp/wave", "/tmp/state.yaml")
			if err != nil {
				t.Fatalf("commandFor() error = %v", err)
			}
			if !reflect.DeepEqual(command.Args, tt.want) {
				t.Fatalf("command args = %#v, want %#v", command.Args, tt.want)
			}
		})
	}
}

func TestModelSelectsWaveArchiveBeforePreview(t *testing.T) {
	model := InitialModel("1.1.1")
	model.cursor = 1
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || updated.(Model).pending != applyCmd {
		t.Fatal("preview did not start archive discovery")
	}
	updated, _ = updated.(Model).Update(archivesMsg{paths: []string{"/tmp/a.wave", "/tmp/b.wave"}})
	picker := updated.(Model)
	if picker.screen != pickerScreen || !strings.Contains(picker.View(), "a.wave") || !strings.Contains(picker.View(), "b.wave") {
		t.Fatalf("picker = %#v\n%s", picker, picker.View())
	}
	picker.archiveCursor = 1
	picker.run = func(cmd cmdType, path string) tea.Cmd {
		if cmd != applyCmd || path != "/tmp/b.wave" {
			t.Fatalf("selection = %s %s", cmd, path)
		}
		return commandResult(cmd, nil)
	}
	_, command = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("selected archive did not run preview")
	}
}

func TestModelShowsCommandOutputInsideTUI(t *testing.T) {
	model := InitialModel("1.1.1")
	updated, _ := model.Update(cmdMsg{cmd: applyCmd, output: "Migration Preview Summary\n- .vimrc"})
	result := updated.(Model)
	if result.screen != outputScreen || !strings.Contains(result.View(), "- .vimrc") {
		t.Fatalf("result screen = %#v\n%s", result, result.View())
	}
}

func TestInventoryGroupsAreCollapsedAndToggleable(t *testing.T) {
	inventory := bundle.Inventory{Groups: []bundle.InventoryGroup{
		{Name: "Applications", WillMigrate: []string{"jq"}, WillNotMigrate: []string{"Manual Tool"}},
		{Name: "Dotfiles", WillMigrate: []string{".vimrc"}},
		{Name: "Settings", WillMigrate: []string{"Dock autohide = true"}},
	}}
	model := InitialModel("1.2.0")
	updated, _ := model.Update(cmdMsg{cmd: viewCmd, inventory: &inventory})
	result := updated.(Model)
	if result.screen != inventoryScreen || strings.Contains(result.View(), "Manual Tool") || !strings.Contains(result.View(), "▸ Applications") {
		t.Fatalf("collapsed inventory = %s", result.View())
	}
	updated, _ = result.Update(tea.KeyMsg{Type: tea.KeyEnter})
	expanded := updated.(Model).View()
	if !strings.Contains(expanded, "▾ Applications") || !strings.Contains(expanded, "Manual Tool") {
		t.Fatalf("expanded inventory = %s", expanded)
	}
}

func TestPortableViewRendersReadableArchive(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	source := filepath.Join(homeDir, ".vimrc")
	if err := os.WriteFile(source, []byte("set number\n"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(homeDir, "selected.wave")
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: source}}}}
	if err := bundle.Create(archive, homeDir, state); err != nil {
		t.Fatal(err)
	}
	message := runPortableCommand(viewCmd, archive)()
	result, ok := message.(cmdMsg)
	if !ok || result.err != nil || result.inventory == nil || len(result.inventory.Groups) != 3 || !strings.Contains(result.inventory.Groups[1].WillMigrate[0], ".vimrc") {
		t.Fatalf("view result = %#v", message)
	}
}
