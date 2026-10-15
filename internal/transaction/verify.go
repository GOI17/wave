package transaction

import (
	"fmt"
	"sort"
	"strings"

	"wave/internal/analyzer"
	"wave/internal/bundle"
	"wave/internal/models"
)

// Verification only describes the state represented in the archive. An archive
// does not contain a full disk inventory, so it cannot prove a whole-Mac clone.
type Verification struct {
	Matched     int      `json:"matched"`
	Differences []string `json:"differences"`
	Unverified  []string `json:"unverified"`
}

func (v *Verification) Complete() bool { return len(v.Differences) == 0 && len(v.Unverified) == 0 }

func Verify(path, home string) (*Verification, error) {
	return VerifyWithSystem(path, home, execSystem{})
}

// VerifyWithSystem never invokes System.Run or writes transaction state.
func VerifyWithSystem(path, home string, system System) (*Verification, error) {
	opened, err := bundle.Open(path)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	result := &Verification{Differences: []string{}, Unverified: []string{}}
	for _, file := range opened.Manifest.Files {
		destination, err := destinationPath(home, file.Destination)
		if err != nil {
			return nil, err
		}
		hash, mode, err := fileStateRegular(destination)
		if err != nil {
			result.Differences = append(result.Differences, "file unavailable or not regular: "+file.Destination)
		} else if hash != file.SHA256 || mode != file.Mode.Perm() {
			result.Differences = append(result.Differences, "file content or permissions differ: "+file.Destination)
		} else {
			result.Matched++
		}
	}
	if opened.Manifest.Excluded > 0 {
		result.Unverified = append(result.Unverified, fmt.Sprintf("%d source files were excluded from the archive", opened.Manifest.Excluded))
	}
	state := opened.Manifest.State
	for _, spec := range preferenceSpecs(state) {
		actual, err := readPreference(system, spec.Domain, spec.Key)
		if err != nil || !actual.Existed {
			result.Differences = append(result.Differences, "preference unavailable: "+spec.Domain+":"+spec.Key)
		} else if actual.Type != spec.Type || actual.Value != normalizePreference(spec.Type, spec.Value) {
			result.Differences = append(result.Differences, "preference differs: "+spec.Domain+":"+spec.Key)
		} else {
			result.Matched++
		}
	}
	settings := []struct{ kind, value string }{
		{"computer-name", state.Preferences.System.ComputerName},
		{"time-zone", state.Preferences.System.TimeZone},
		{"language", state.Preferences.System.Language},
	}
	for _, setting := range settings {
		if setting.value == "" {
			continue
		}
		actual, err := readSystemSetting(system, setting.kind)
		if err != nil || strings.TrimSpace(actual) != setting.value {
			result.Differences = append(result.Differences, "system setting differs or unavailable: "+setting.kind)
		} else {
			result.Matched++
		}
	}
	for _, extension := range state.Applications.VSCodeExtensions {
		_, installed, err := packageStatus(system, "vscode", extension, "extension")
		if err != nil || !installed {
			result.Differences = append(result.Differences, "VS Code extension missing or unavailable: "+extension)
		} else {
			result.Matched++
		}
	}
	if len(state.Applications.VSCodeExtensions) > 0 {
		result.Unverified = append(result.Unverified, "VS Code extension versions were not captured")
	}
	for _, app := range state.Applications.AppStore {
		_, installed, err := packageStatus(system, "mas", app.BundleID, "app-store")
		if err != nil || !installed {
			result.Differences = append(result.Differences, "App Store application missing or unavailable: "+app.Name)
		} else {
			result.Matched++
		}
		result.Unverified = append(result.Unverified, "App Store application version and data: "+app.Name)
	}
	for _, app := range state.Applications.Manual {
		result.Unverified = append(result.Unverified, "manual application has no install payload: "+app.Name)
	}
	// AnalyzeApplications also checks installed versions and fails explicitly on
	// inventory errors. It has no mutation operations.
	if len(state.Applications.Homebrew) > 0 {
		actual, err := analyzer.NewMacOSAnalyzer(home).AnalyzeApplications()
		if err != nil {
			result.Unverified = append(result.Unverified, "Homebrew inventory could not be read")
		} else {
			compareHomebrew(result, state.Applications.Homebrew, actual.Homebrew)
		}
	}
	if state.Preferences.Trackpad != (models.TrackpadPrefs{}) || state.Preferences.Keyboard.NumLock || state.Preferences.System.ScreenBrightness != 0 || len(state.Preferences.Dock.AppOrder) > 0 || len(state.Preferences.Dock.PersistentApps) > 0 || len(state.Preferences.Apps) > 0 {
		result.Unverified = append(result.Unverified, "archive contains preferences not supported by Apply")
	}
	return result, nil
}

func compareHomebrew(result *Verification, expected, actual []models.HomebrewPackage) {
	installed := make(map[string]models.HomebrewPackage)
	for _, pkg := range actual {
		installed[pkg.Type+":"+pkg.Name] = pkg
	}
	for _, pkg := range expected {
		key := pkg.Type + ":" + pkg.Name
		got, exists := installed[key]
		if !exists {
			result.Differences = append(result.Differences, "Homebrew package missing: "+key)
			continue
		}
		if pkg.Version == "" {
			result.Unverified = append(result.Unverified, "Homebrew version was not captured: "+key)
			continue
		}
		if normalizedVersions(pkg.Name, pkg.Version) != normalizedVersions(got.Name, got.Version) || pkg.Pinned != got.Pinned {
			result.Differences = append(result.Differences, "Homebrew version or pin differs: "+key)
		} else {
			result.Matched++
		}
	}
}

func normalizedVersions(name, value string) string {
	fields := strings.Fields(value)
	if len(fields) > 0 && fields[0] == name {
		fields = fields[1:]
	}
	sort.Strings(fields)
	return strings.Join(fields, " ")
}

func FormatVerification(result *Verification) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Archive verification: %d matched, %d differences, %d unverified\n", result.Matched, len(result.Differences), len(result.Unverified))
	for _, difference := range result.Differences {
		fmt.Fprintf(&out, "DIFFERENT: %s\n", difference)
	}
	for _, item := range result.Unverified {
		fmt.Fprintf(&out, "UNVERIFIED: %s\n", item)
	}
	out.WriteString("Scope: captured archive items only; documents, application data, ACLs, extended attributes and full-system identity are not certified by this archive format.\n")
	return out.String()
}
