package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	docStyle = lipgloss.NewStyle().
			Margin(1, 2).
			Padding(1, 2).
			Background(lipgloss.Color("#2B2B2B")).
			Foreground(lipgloss.Color("#A9B7C6"))
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CC7832")).
			Background(lipgloss.Color("#2B2B2B")).
			Bold(true)
	highlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6897BB")).
			Background(lipgloss.Color("#2B2B2B")).
			Bold(true)
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080")).
			Background(lipgloss.Color("#2B2B2B"))
)

// Model represents the TUI state
type Model struct {
	currentStep int
	choices     []string
	cursor      int
	selected    map[int]bool
	status      string
	run         func(cmdType) tea.Cmd
	version     string
	width       int
	height      int
}

// InitialModel creates a new model
func InitialModel(version string) Model {
	return Model{
		currentStep: 0,
		choices: []string{
			"Capture Device State",
			"Preview Migration (Dry Run)",
			"View Captured State",
			"Verify Migration",
			"Exit",
		},
		selected: make(map[int]bool),
		run:      runCommand,
		version:  version,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case cmdMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("%s failed: %v", commandLabel(msg.cmd), msg.err)
		} else {
			m.status = fmt.Sprintf("%s completed", commandLabel(msg.cmd))
		}
		return m, nil

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
				m.status = "Starting capture..."
				return m, m.run(captureCmd)
			case 1:
				m.status = "Starting migration preview..."
				return m, m.run(applyCmd)
			case 2:
				m.status = "Opening captured state..."
				return m, m.run(viewCmd)
			case 3:
				m.status = "Starting verification..."
				return m, m.run(verifyCmd)
			case 4:
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// View implements tea.Model
func (m Model) View() string {
	s := titleStyle.Render(fmt.Sprintf("🌊 Wave v%s – macOS Device Migrator", m.version)) + "\n\n"
	s += "Choose an action:\n\n"

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = highlightStyle.Render("❯")
		}

		s += fmt.Sprintf("%s %s\n", cursor, choice)
	}

	s += "\n" + mutedStyle.Render("Use arrow keys or j/k to navigate, enter to select, q to quit") + "\n"
	if m.status != "" {
		s += "\n" + m.status + "\n"
	}

	style := docStyle
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width - 4).Height(m.height - 2)
	}
	return style.Render(s)
}

// cmdType represents a command to execute
type cmdType string

const (
	captureCmd cmdType = "capture"
	applyCmd   cmdType = "apply"
	viewCmd    cmdType = "view"
	verifyCmd  cmdType = "verify"
)

// runCommand suspends the TUI while the selected workflow uses the terminal.
func runCommand(cmd cmdType) tea.Cmd {
	executable, err := os.Executable()
	if err != nil {
		return commandResult(cmd, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return commandResult(cmd, err)
	}

	process, err := commandFor(cmd, executable, filepath.Join(homeDir, "wave-state.yaml"))
	if err != nil {
		return commandResult(cmd, err)
	}

	return tea.ExecProcess(process, func(err error) tea.Msg {
		return cmdMsg{cmd: cmd, err: err}
	})
}

func commandFor(cmd cmdType, executable, statePath string) (*exec.Cmd, error) {
	switch cmd {
	case captureCmd:
		return exec.Command(executable, "capture", "--output", statePath), nil
	case applyCmd:
		return exec.Command(executable, "apply", "--input", statePath, "--dry-run"), nil
	case viewCmd:
		return exec.Command("/usr/bin/less", statePath), nil
	case verifyCmd:
		return exec.Command(executable, "verify", "--input", statePath), nil
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
}

func commandResult(cmd cmdType, err error) tea.Cmd {
	return func() tea.Msg {
		return cmdMsg{cmd: cmd, err: err}
	}
}

func commandLabel(cmd cmdType) string {
	switch cmd {
	case captureCmd:
		return "Capture"
	case applyCmd:
		return "Migration"
	case viewCmd:
		return "State viewer"
	case verifyCmd:
		return "Verification"
	default:
		return "Command"
	}
}

// cmdMsg represents the result of a command
type cmdMsg struct {
	cmd cmdType
	err error
}

// StartTUI launches the TUI
func StartTUI(version string) error {
	p := tea.NewProgram(InitialModel(version), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running tui: %w", err)
	}
	return nil
}
