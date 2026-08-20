package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"wave/internal/bundle"
	"wave/internal/migrator"
	"wave/internal/transaction"
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
	currentStep   int
	choices       []string
	cursor        int
	selected      map[int]bool
	status        string
	run           func(cmdType, string) tea.Cmd
	version       string
	width         int
	height        int
	pending       cmdType
	screen        screenType
	archives      []string
	archive       string
	archiveCursor int
	output        string
	outputOffset  int
}

type screenType string

const (
	menuScreen   screenType = "menu"
	pickerScreen screenType = "picker"
	outputScreen screenType = "output"
)

// InitialModel creates a new model
func InitialModel(version string) Model {
	return Model{
		currentStep: 0,
		choices: []string{
			"Capture Device State",
			"Preview Migration (Dry Run)",
			"Apply Migration",
			"Rollback Latest Migration",
			"View Captured Archive",
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
		if msg.output != "" {
			m.output = msg.output
			m.outputOffset = 0
			m.screen = outputScreen
		}
		return m, nil

	case archivesMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.archives = msg.paths
		m.archiveCursor = 0
		m.screen = pickerScreen
		return m, nil

	case tea.KeyMsg:
		if m.screen == outputScreen {
			switch msg.String() {
			case "esc", "q", "enter":
				m.screen = menuScreen
				m.output = ""
			case "up", "k":
				if m.outputOffset > 0 {
					m.outputOffset--
				}
			case "down", "j":
				if m.outputOffset < len(strings.Split(m.output, "\n"))-1 {
					m.outputOffset++
				}
			}
			return m, nil
		}
		if m.screen == pickerScreen {
			switch msg.String() {
			case "esc", "q":
				m.screen = menuScreen
				m.pending = ""
			case "up", "k":
				if m.archiveCursor > 0 {
					m.archiveCursor--
				}
			case "down", "j":
				if m.archiveCursor < len(m.archives)-1 {
					m.archiveCursor++
				}
			case "enter", " ":
				if len(m.archives) == 0 {
					return m, nil
				}
				m.archive = m.archives[m.archiveCursor]
				m.screen = menuScreen
				if m.pending == liveApplyCmd {
					m.status = fmt.Sprintf("Apply %s? Press y to confirm or n to cancel.", filepath.Base(m.archive))
					return m, nil
				}
				pending := m.pending
				m.pending = ""
				m.status = fmt.Sprintf("Starting %s...", strings.ToLower(commandLabel(pending)))
				return m, m.run(pending, m.archive)
			}
			return m, nil
		}
		if m.pending != "" {
			switch msg.String() {
			case "y", "Y":
				pending := m.pending
				m.pending = ""
				m.status = fmt.Sprintf("Starting %s...", strings.ToLower(commandLabel(pending)))
				return m, m.run(pending, m.archive)
			case "n", "N", "esc", "q":
				m.pending = ""
				m.status = "Operation cancelled"
				return m, nil
			default:
				return m, nil
			}
		}
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
				return m, m.run(captureCmd, "")
			case 1:
				m.pending = applyCmd
				return m, discoverArchives()
			case 2:
				m.pending = liveApplyCmd
				return m, discoverArchives()
			case 3:
				m.pending = rollbackCmd
				m.status = "Rollback the latest migration? Press y to confirm or n to cancel."
				return m, nil
			case 4:
				m.pending = viewCmd
				return m, discoverArchives()
			case 5:
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// View implements tea.Model
func (m Model) View() string {
	s := titleStyle.Render(fmt.Sprintf("🌊 Wave v%s – macOS Device Migrator", m.version)) + "\n\n"
	if m.screen == pickerScreen {
		s += "Choose a portable archive:\n\n"
		for i, path := range m.archives {
			cursor := " "
			if i == m.archiveCursor {
				cursor = highlightStyle.Render("❯")
			}
			s += fmt.Sprintf("%s %s\n", cursor, path)
		}
		if len(m.archives) == 0 {
			s += "No .wave archives found in your home directory.\n"
		}
		s += "\n" + mutedStyle.Render("Use j/k to navigate, enter to select, esc to go back") + "\n"
		return m.render(s)
	}
	if m.screen == outputScreen {
		s += m.visibleOutput()
		s += "\n" + mutedStyle.Render("Use j/k to scroll, enter or esc to go back") + "\n"
		return m.render(s)
	}
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

	return m.render(s)
}

func (m Model) render(content string) string {
	style := docStyle
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width - 4).Height(m.height - 2)
	}
	return style.Render(content)
}

func (m Model) visibleOutput() string {
	lines := strings.Split(m.output, "\n")
	limit := len(lines)
	if m.height > 8 && limit > m.height-8 {
		limit = m.height - 8
	}
	end := m.outputOffset + limit
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[m.outputOffset:end], "\n") + "\n"
}

// cmdType represents a command to execute
type cmdType string

const (
	captureCmd   cmdType = "capture"
	applyCmd     cmdType = "apply"
	liveApplyCmd cmdType = "live-apply"
	rollbackCmd  cmdType = "rollback"
	viewCmd      cmdType = "view"
)

// runCommand suspends the TUI while the selected workflow uses the terminal.
func runCommand(cmd cmdType, archivePath string) tea.Cmd {
	if cmd == applyCmd || cmd == liveApplyCmd || cmd == viewCmd || cmd == rollbackCmd {
		return runPortableCommand(cmd, archivePath)
	}
	executable, err := os.Executable()
	if err != nil {
		return commandResult(cmd, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return commandResult(cmd, err)
	}

	process, err := commandFor(cmd, executable, filepath.Join(homeDir, "wave-state.wave"))
	if err != nil {
		return commandResult(cmd, err)
	}

	return tea.ExecProcess(process, func(err error) tea.Msg {
		return cmdMsg{cmd: cmd, err: err}
	})
}

func runPortableCommand(cmd cmdType, archivePath string) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return cmdMsg{cmd: cmd, err: err}
		}
		switch cmd {
		case applyCmd:
			result, err := transaction.Preview(archivePath)
			if err != nil {
				return cmdMsg{cmd: cmd, err: err}
			}
			opened, err := bundle.Open(archivePath)
			if err != nil {
				return cmdMsg{cmd: cmd, err: err}
			}
			defer opened.Close()
			return cmdMsg{cmd: cmd, output: migrator.FormatSummary(result) + "\n" + bundle.FormatSummary(opened)}
		case liveApplyCmd:
			journal, err := transaction.Apply(archivePath, homeDir, filepath.Join(homeDir, ".wave", "transactions"))
			if err != nil {
				return cmdMsg{cmd: cmd, err: err}
			}
			return cmdMsg{cmd: cmd, output: transaction.FormatApplySummary(journal)}
		case rollbackCmd:
			result, err := transaction.RollbackLatest(homeDir, filepath.Join(homeDir, ".wave", "transactions"))
			if err != nil {
				return cmdMsg{cmd: cmd, err: err}
			}
			return cmdMsg{cmd: cmd, output: transaction.FormatRollbackSummary(result)}
		case viewCmd:
			opened, err := bundle.Open(archivePath)
			if err != nil {
				return cmdMsg{cmd: cmd, err: err}
			}
			defer opened.Close()
			for _, file := range opened.Manifest.Files {
				if _, err := opened.ReadFile(file); err != nil {
					return cmdMsg{cmd: cmd, err: err}
				}
			}
			return cmdMsg{cmd: cmd, output: bundle.FormatSummary(opened)}
		default:
			return cmdMsg{cmd: cmd, err: fmt.Errorf("unsupported portable command: %s", cmd)}
		}
	}
}

func discoverArchives() tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return archivesMsg{err: err}
		}
		seen := make(map[string]bool)
		var matches []string
		for _, directory := range []string{homeDir, filepath.Join(homeDir, "Downloads"), filepath.Join(homeDir, "Desktop"), filepath.Join(homeDir, "Documents")} {
			paths, err := filepath.Glob(filepath.Join(directory, "*.wave"))
			if err != nil {
				return archivesMsg{err: err}
			}
			for _, path := range paths {
				if !seen[path] {
					seen[path] = true
					matches = append(matches, path)
				}
			}
		}
		sort.Strings(matches)
		return archivesMsg{paths: matches}
	}
}

func commandFor(cmd cmdType, executable, statePath string) (*exec.Cmd, error) {
	switch cmd {
	case captureCmd:
		return exec.Command(executable, "capture", "--output", statePath), nil
	case applyCmd:
		return exec.Command(executable, "apply", "--input", statePath, "--dry-run"), nil
	case liveApplyCmd:
		return exec.Command(executable, "apply", "--input", statePath, "--confirm"), nil
	case rollbackCmd:
		return exec.Command(executable, "rollback", "--confirm"), nil
	case viewCmd:
		return exec.Command("/usr/bin/less", statePath), nil
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
		return "Preview"
	case liveApplyCmd:
		return "Migration"
	case rollbackCmd:
		return "Rollback"
	case viewCmd:
		return "State viewer"
	default:
		return "Command"
	}
}

// cmdMsg represents the result of a command
type cmdMsg struct {
	cmd    cmdType
	output string
	err    error
}

type archivesMsg struct {
	paths []string
	err   error
}

// StartTUI launches the TUI
func StartTUI(version string) error {
	p := tea.NewProgram(InitialModel(version), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running tui: %w", err)
	}
	return nil
}
