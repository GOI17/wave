package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Bold(true)
	highlightStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("212")).
		Bold(true)
)

// Model represents the TUI state
type Model struct {
	currentStep int
	choices     []string
	cursor      int
	selected    map[int]bool
}

// InitialModel creates a new model
func InitialModel() Model {
	return Model{
		currentStep: 0,
		choices: []string{
			"Capture Device State",
			"Apply Migration",
			"View Captured State",
			"Verify Migration",
			"Exit",
		},
		selected: make(map[int]bool),
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			switch m.cursor {
			case 0:
				return m, runCommand("capture")
			case 1:
				return m, runCommand("apply")
			case 2:
				return m, runCommand("view")
			case 3:
				return m, runCommand("verify")
			case 4:
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// View implements tea.Model
func (m Model) View() string {
	s := titleStyle.Render("🌊 Wave – macOS Device Migrator") + "\n\n"
	s += "Choose an action:\n\n"

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = highlightStyle.Render("❯")
		}

		s += fmt.Sprintf("%s %s\n", cursor, choice)
	}

	s += "\nUse arrow keys or j/k to navigate, enter to select, q to quit\n"

	return docStyle.Render(s)
}

// cmdType represents a command to execute
type cmdType string

const (
	captureCmd cmdType = "capture"
	applyCmd   cmdType = "apply"
	viewCmd    cmdType = "view"
	verifyCmd  cmdType = "verify"
)

// RunCmd represents a command to run
func runCommand(cmd string) tea.Cmd {
	return func() tea.Msg {
		return cmdMsg{cmd: cmd}
	}
}

// cmdMsg represents the result of a command
type cmdMsg struct {
	cmd string
	err error
}

// StartTUI launches the TUI
func StartTUI() error {
	p := tea.NewProgram(InitialModel())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running tui: %w", err)
	}
	return nil
}
