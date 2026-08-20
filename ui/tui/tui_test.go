package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestModelRunsSelectedAction(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		want   cmdType
	}{
		{name: "capture", cursor: 0, want: captureCmd},
		{name: "apply", cursor: 1, want: applyCmd},
		{name: "view", cursor: 4, want: viewCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("1.0.1")
			model.cursor = tt.cursor
			model.run = func(cmd cmdType) tea.Cmd {
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
	}{{name: "apply", cursor: 2, want: liveApplyCmd}, {name: "rollback", cursor: 3, want: rollbackCmd}} {
		t.Run(tt.name, func(t *testing.T) {
			model := InitialModel("1.0.3")
			model.cursor = tt.cursor
			called := false
			model.run = func(cmd cmdType) tea.Cmd {
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

func TestModelShowsCommandError(t *testing.T) {
	model := InitialModel("1.0.1")
	model.run = func(cmd cmdType) tea.Cmd {
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
		{name: "apply", cmd: applyCmd, want: []string{"/tmp/wave", "apply", "--input", "/tmp/state.yaml", "--dry-run"}},
		{name: "live apply", cmd: liveApplyCmd, want: []string{"/tmp/wave", "apply", "--input", "/tmp/state.yaml", "--confirm"}},
		{name: "rollback", cmd: rollbackCmd, want: []string{"/tmp/wave", "rollback", "--confirm"}},
		{name: "view", cmd: viewCmd, want: []string{"/usr/bin/less", "/tmp/state.yaml"}},
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
