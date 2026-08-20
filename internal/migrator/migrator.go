package migrator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"wave/internal/analyzer"
	"wave/internal/bundle"
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
	state, err := m.Analyze()
	if err != nil {
		return err
	}

	// Save to file
	return m.saveState(state, outputPath, format)
}

// Analyze collects current device state without writing it.
func (m *Migrator) Analyze() (*models.MigrationState, error) {
	// Capture device info
	state, err := m.analyzer.AnalyzeDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze device: %w", err)
	}

	// Capture applications
	apps, err := m.analyzer.AnalyzeApplications()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze applications: %w", err)
	}
	state.Applications = *apps

	// Capture dotfiles
	dotfiles, err := m.analyzer.AnalyzeDotfiles()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze dotfiles: %w", err)
	}
	state.Dotfiles = *dotfiles

	// Capture preferences
	prefs, err := m.analyzer.AnalyzePreferences()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze preferences: %w", err)
	}
	state.Preferences = *prefs

	// Capture environment
	env, err := m.analyzer.AnalyzeEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to analyze environment: %w", err)
	}
	state.Environment = *env

	return state, nil
}

// CaptureBundle writes a portable .wave archive including safe file contents.
func (m *Migrator) CaptureBundle(outputPath, homeDir string) error {
	state, err := m.Analyze()
	if err != nil {
		return err
	}
	return bundle.Create(outputPath, homeDir, state)
}

// Apply loads state from file and applies to target device.
func (m *Migrator) Apply(inputPath string, dryRun bool, format string) (*models.MigrationResult, error) {
	state, err := m.loadState(inputPath, format)
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	// Validate state
	if err := m.executor.ValidateState(state, dryRun); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	result := &models.MigrationResult{DryRun: dryRun, Warnings: []string{}}

	// Execute applications
	appTasks, err := m.executor.ExecuteApplications(&state.Applications, dryRun)
	addCategory(result, "Applications", appTasks)
	if err != nil {
		return result, fmt.Errorf("failed to execute applications: %w", err)
	}

	// Execute dotfiles
	dotfileTasks, err := m.executor.ExecuteDotfiles(&state.Dotfiles, dryRun)
	addCategory(result, "Dotfiles", dotfileTasks)
	if err != nil {
		return result, fmt.Errorf("failed to execute dotfiles: %w", err)
	}

	// Execute preferences
	prefTasks, err := m.executor.ExecutePreferences(&state.Preferences, dryRun)
	addCategory(result, "Preferences", prefTasks)
	if err != nil {
		return result, fmt.Errorf("failed to execute preferences: %w", err)
	}

	// Execute environment
	envTasks, err := m.executor.ExecuteEnvironment(&state.Environment, dryRun)
	addCategory(result, "Environment", envTasks)
	if err != nil {
		return result, fmt.Errorf("failed to execute environment: %w", err)
	}

	return result, nil
}

func addCategory(result *models.MigrationResult, name string, tasks []models.MigrationTask) {
	category := models.MigrationCategoryResult{Name: name, Total: len(tasks)}
	for _, task := range tasks {
		switch task.Status {
		case "success":
			category.Successful++
			result.Successful++
		case "skipped":
			category.Skipped++
			result.Skipped++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", task.Name, task.Error))
		case "failed":
			category.Failed++
			result.Failed++
		}
	}
	result.Total += len(tasks)
	result.Categories = append(result.Categories, category)
}

// FormatSummary renders the canonical migration summary used by every UI.
func FormatSummary(result *models.MigrationResult) string {
	var summary strings.Builder
	summary.WriteString("Migration Preview Summary\n")
	summary.WriteString("=========================\n")
	for _, category := range result.Categories {
		fmt.Fprintf(&summary, "%-12s %3d total  %3d ready  %3d skipped  %3d failed\n",
			category.Name+":", category.Total, category.Successful, category.Skipped, category.Failed)
	}
	fmt.Fprintf(&summary, "%-12s %3d total  %3d ready  %3d skipped  %3d failed\n",
		"Total:", result.Total, result.Successful, result.Skipped, result.Failed)
	if len(result.Warnings) > 0 {
		summary.WriteString("\nWarnings:\n")
		for _, warning := range result.Warnings {
			fmt.Fprintf(&summary, "- %s\n", warning)
		}
	}
	if result.DryRun {
		summary.WriteString("\nDry-run completed. No changes were applied.\n")
	} else {
		summary.WriteString("\nMigration completed successfully.\n")
	}
	return summary.String()
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
