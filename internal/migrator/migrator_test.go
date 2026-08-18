package migrator

import (
	"strings"
	"testing"

	"wave/internal/models"
)

func TestFormatSummary(t *testing.T) {
	result := &models.MigrationResult{
		DryRun: true,
		Categories: []models.MigrationCategoryResult{
			{Name: "Applications", Total: 2, Successful: 2},
			{Name: "Dotfiles", Total: 3, Successful: 2, Skipped: 1},
			{Name: "Preferences", Total: 1, Successful: 1},
			{Name: "Environment", Total: 1, Successful: 1},
		},
		Total:      7,
		Successful: 6,
		Skipped:    1,
		Warnings:   []string{"Copy config.lua: source unavailable"},
	}

	summary := FormatSummary(result)
	for _, expected := range []string{
		"Migration Preview Summary",
		"Applications:",
		"Dotfiles:",
		"7 total",
		"6 ready",
		"1 skipped",
		"Copy config.lua: source unavailable",
		"Dry-run completed. No changes were applied.",
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("summary does not contain %q:\n%s", expected, summary)
		}
	}
}
