package analyzer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"wave/internal/models"
)

// Capture installed versions, including dependencies. "brew leaves" loses
// installed packages, and "brew list --casks --json" is not a valid inventory.
func captureHomebrew(kind string) ([]models.HomebrewPackage, error) {
	brew, err := exec.LookPath("brew")
	if errors.Is(err, exec.ErrNotFound) {
		return []models.HomebrewPackage{}, nil
	}
	if err != nil {
		return nil, err
	}
	output, err := exec.Command(brew, "list", "--"+kind, "--versions", "--json").Output()
	if err != nil {
		// Older Homebrew releases do not support JSON for list. Only fall back
		// for an unsupported option, never hide an inventory/permission error.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.ToLower(string(exitErr.Stderr))
			if strings.Contains(message, "--json") && (strings.Contains(message, "invalid option") || strings.Contains(message, "unknown option")) {
				return captureHomebrewText(brew, kind)
			}
		}
		return nil, fmt.Errorf("capture Homebrew %s inventory: %w", kind, err)
	}
	var inventory map[string]json.RawMessage
	if err := json.Unmarshal(output, &inventory); err != nil {
		return nil, fmt.Errorf("decode Homebrew %s inventory: %w", kind, err)
	}
	key := "formulae"
	if kind == "cask" {
		key = "casks"
	}
	raw, ok := inventory[key]
	if !ok || string(raw) == "null" {
		return nil, fmt.Errorf("Homebrew inventory is missing %s", key)
	}
	var entries []struct {
		Name          string   `json:"name"`
		Token         string   `json:"token"`
		Versions      []string `json:"versions"`
		PinnedVersion *string  `json:"pinned_version"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode installed Homebrew %s: %w", kind, err)
	}
	packages := make([]models.HomebrewPackage, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if kind == "cask" {
			name = entry.Token
		}
		if name == "" || len(entry.Versions) == 0 {
			return nil, fmt.Errorf("Homebrew %s inventory contains an incomplete entry", kind)
		}
		sort.Strings(entry.Versions)
		packages = append(packages, models.HomebrewPackage{Name: name, Type: kind, Version: strings.Join(entry.Versions, " "), Pinned: entry.PinnedVersion != nil})
	}
	return packages, nil
}

func captureHomebrewText(brew, kind string) ([]models.HomebrewPackage, error) {
	output, err := exec.Command(brew, "list", "--"+kind, "--versions").Output()
	if err != nil {
		return nil, fmt.Errorf("capture Homebrew %s inventory: %w", kind, err)
	}
	packages := []models.HomebrewPackage{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return nil, fmt.Errorf("Homebrew %s inventory contains an incomplete entry", kind)
		}
		packages = append(packages, models.HomebrewPackage{Name: fields[0], Type: kind, Version: strings.Join(fields[1:], " ")})
	}
	return packages, nil
}
