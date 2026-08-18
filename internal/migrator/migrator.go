package migrator

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"wave/internal/analyzer"
	"wave/internal/executor"
	"wave/internal/models"
)

// Migrator orchestrates the migration process
type Migrator struct {
	analyzer analyzer.Analyzer
	executor executor.Executor
}

// NewMigrator creates a new migrator instance
func NewMigrator(analyzer analyzer.Analyzer, executor executor.Executor) *Migrator {
	return &Migrator{
		analyzer: analyzer,
		executor: executor,
	}
}

// Capture collects device state and saves to file
func (m *Migrator) Capture(outputPath string, format string) error {
	// Capture device info
	state, err := m.analyzer.AnalyzeDevice()
	if err != nil {
		return fmt.Errorf("failed to analyze device: %w", err)
	}

	// Capture applications
	apps, err := m.analyzer.AnalyzeApplications()
	if err != nil {
		return fmt.Errorf("failed to analyze applications: %w", err)
	}
	state.Applications = *apps

	// Capture dotfiles
	dotfiles, err := m.analyzer.AnalyzeDotfiles()
	if err != nil {
		return fmt.Errorf("failed to analyze dotfiles: %w", err)
	}
	state.Dotfiles = *dotfiles

	// Capture preferences
	prefs, err := m.analyzer.AnalyzePreferences()
	if err != nil {
		return fmt.Errorf("failed to analyze preferences: %w", err)
	}
	state.Preferences = *prefs

	// Capture environment
	env, err := m.analyzer.AnalyzeEnvironment()
	if err != nil {
		return fmt.Errorf("failed to analyze environment: %w", err)
	}
	state.Environment = *env

	// Save to file
	return m.saveState(state, outputPath, format)
}

// Apply loads state from file and applies to target device
func (m *Migrator) Apply(inputPath string, dryRun bool, format string) error {
	state, err := m.loadState(inputPath, format)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Validate state
	if err := m.executor.ValidateState(state, dryRun); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Execute applications
	appTasks, err := m.executor.ExecuteApplications(&state.Applications, dryRun)
	if err != nil {
		return fmt.Errorf("failed to execute applications: %w", err)
	}
	fmt.Printf("Processed %d application tasks\n", len(appTasks))

	// Execute dotfiles
	dotfileTasks, err := m.executor.ExecuteDotfiles(&state.Dotfiles, dryRun)
	if err != nil {
		return fmt.Errorf("failed to execute dotfiles: %w", err)
	}
	fmt.Printf("Processed %d dotfile tasks\n", len(dotfileTasks))

	// Execute preferences
	prefTasks, err := m.executor.ExecutePreferences(&state.Preferences, dryRun)
	if err != nil {
		return fmt.Errorf("failed to execute preferences: %w", err)
	}
	fmt.Printf("Processed %d preference tasks\n", len(prefTasks))

	// Execute environment
	envTasks, err := m.executor.ExecuteEnvironment(&state.Environment, dryRun)
	if err != nil {
		return fmt.Errorf("failed to execute environment: %w", err)
	}
	fmt.Printf("Processed %d environment tasks\n", len(envTasks))

	if dryRun {
		fmt.Println("\n✓ Dry-run completed. No changes were applied.")
	} else {
		fmt.Println("\n✓ Migration completed successfully.")
	}

	return nil
}

// saveState writes migration state to file
func (m *Migrator) saveState(state *models.MigrationState, path string, format string) error {
	var data []byte
	var err error

	switch format {
	case "yaml", "yml":
		data, err = yaml.Marshal(state)
	case "json":
		data, err = json.MarshalIndent(state, "", "  ")
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	fmt.Printf("Migration state saved to: %s\n", path)
	return nil
}

// loadState reads migration state from file
func (m *Migrator) loadState(path string, format string) (*models.MigrationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	state := &models.MigrationState{}

	switch format {
	case "yaml", "yml":
		err = yaml.Unmarshal(data, state)
	case "json":
		err = json.Unmarshal(data, state)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return state, nil
}
