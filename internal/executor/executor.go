package executor

import (
	"wave/internal/models"
)

// Executor applies migration state to a target device
type Executor interface {
	ExecuteApplications(apps *models.ApplicationGroup, dryRun bool) ([]models.MigrationTask, error)
	ExecuteDotfiles(dotfiles *models.DotfilesGroup, dryRun bool) ([]models.MigrationTask, error)
	ExecutePreferences(prefs *models.PreferencesGroup, dryRun bool) ([]models.MigrationTask, error)
	ExecuteEnvironment(env *models.EnvironmentGroup, dryRun bool) ([]models.MigrationTask, error)
	ValidateState(state *models.MigrationState) error
}

