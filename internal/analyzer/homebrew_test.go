package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeBrew(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "brew"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestHomebrewCasksUseInstalledJSONSchema(t *testing.T) {
	fakeBrew(t, `test "$*" = 'list --cask --versions --json' || exit 9
printf '%s' '{"formulae":[],"casks":[{"token":"docker-desktop","versions":["4.37.2,179585"],"pinned_version":null}]}'
`)
	apps, err := captureHomebrew("cask")
	if err != nil || len(apps) != 1 || apps[0].Name != "docker-desktop" || apps[0].Version != "4.37.2,179585" {
		t.Fatalf("apps=%+v, error=%v", apps, err)
	}
}

func TestHomebrewFormulaInventoryIncludesDependenciesAndVersions(t *testing.T) {
	fakeBrew(t, `test "$*" = 'list --formula --versions --json' || exit 9
printf '%s' '{"formulae":[{"name":"git","versions":["2.55.0"],"pinned_version":"2.55.0"},{"name":"gettext","versions":["1.0","0.26"]}],"casks":[]}'
`)
	apps, err := captureHomebrew("formula")
	if err != nil || len(apps) != 2 || !apps[0].Pinned || apps[1].Version != "0.26 1.0" {
		t.Fatalf("apps=%+v, error=%v", apps, err)
	}
}

func TestHomebrewFailureIsNotAnEmptySuccessfulCapture(t *testing.T) {
	fakeBrew(t, "exit 42\n")
	_, err := (&macOSAnalyzer{homeDir: t.TempDir()}).AnalyzeApplications()
	if err == nil || !strings.Contains(err.Error(), "Homebrew") {
		t.Fatalf("error=%v", err)
	}
}

func TestHomebrewRejectsMalformedInventory(t *testing.T) {
	for _, payload := range []string{`[]`, `{}`, `{"casks":null}`, `{"casks":[{"token":"app"}]}`} {
		t.Run(payload, func(t *testing.T) {
			fakeBrew(t, "printf '%s' '"+payload+"'\n")
			if _, err := captureHomebrew("cask"); err == nil {
				t.Fatal("accepted incomplete inventory")
			}
		})
	}
}

func TestOlderHomebrewFallsBackOnlyForUnsupportedJSON(t *testing.T) {
	fakeBrew(t, `if [ "$*" = 'list --formula --versions --json' ]; then
  echo 'Error: invalid option: --json' >&2
  exit 1
fi
test "$*" = 'list --formula --versions' || exit 9
printf 'git 2.55.0\ngettext 1.0\n'
`)
	apps, err := captureHomebrew("formula")
	if err != nil || len(apps) != 2 || apps[0].Version != "2.55.0" {
		t.Fatalf("apps=%+v, error=%v", apps, err)
	}
}
