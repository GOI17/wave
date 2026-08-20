package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetManualAppsCapturesUserApplicationMetadata(t *testing.T) {
	homeDir := t.TempDir()
	app := filepath.Join(homeDir, "Applications", "Example.app")
	if err := os.MkdirAll(app, 0700); err != nil {
		t.Fatal(err)
	}
	analyzer := &macOSAnalyzer{homeDir: homeDir}
	apps := analyzer.scanManualApps([]string{filepath.Join(homeDir, "Applications")}, nil, nil)
	if len(apps) != 1 || apps[0].Name != "Example" || apps[0].Path != app {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestLanguageCaptureReturnsPrimaryLanguage(t *testing.T) {
	for input, want := range map[string]string{
		`("en-US", "es-US")`: "en-US,es-US",
		`("fr")`:             "fr",
	} {
		if got := normalizeCapturedLanguage(input); got != want {
			t.Fatalf("normalizeCapturedLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}
