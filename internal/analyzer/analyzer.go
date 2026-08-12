package analyzer

import (
	"wave/internal/models"
)

// Analyzer captures the current device state
type Analyzer interface {
	AnalyzeDevice() (*models.MigrationState, error)
	AnalyzeApplications() (*models.ApplicationGroup, error)
	AnalyzeDotfiles() (*models.DotfilesGroup, error)
	AnalyzePreferences() (*models.PreferencesGroup, error)
	AnalyzeEnvironment() (*models.EnvironmentGroup, error)
}

