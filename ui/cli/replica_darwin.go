//go:build darwin

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"wave/internal/replica"
)

func init() {
	command := &cobra.Command{Use: "replica", Short: "Capture, restore and verify selected documents and app bundles"}
	var output, input string
	var documents, applications []string
	var preview, confirmed bool
	locations := func() (replica.Locations, error) {
		home, err := os.UserHomeDir()
		return replica.Locations{Home: home, Applications: "/Applications"}, err
	}
	capture := &cobra.Command{Use: "capture", Short: "Create a private directory snapshot of explicitly selected items", RunE: func(cmd *cobra.Command, args []string) error {
		if output == "" {
			return fmt.Errorf("replica capture requires --output")
		}
		roots, err := locations()
		if err != nil {
			return err
		}
		items := []replica.Selection{}
		for _, path := range documents {
			items = append(items, replica.Selection{Root: "home", Path: path})
		}
		for _, path := range applications {
			items = append(items, replica.Selection{Root: "applications", Path: path})
		}
		if err := replica.Capture(output, items, roots); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Selected items captured. Keep the entire snapshot directory private.")
		return nil
	}}
	capture.Flags().StringVar(&output, "output", "", "New snapshot directory")
	capture.Flags().StringArrayVar(&documents, "document", nil, "Path relative to home below Documents, Desktop, Downloads, Pictures, Movies or Music (repeatable)")
	capture.Flags().StringArrayVar(&applications, "application", nil, "Installed .app bundle name in /Applications (repeatable)")
	apply := &cobra.Command{Use: "apply", Short: "Restore selected items without overwriting different existing data", RunE: func(cmd *cobra.Command, args []string) error {
		if input == "" {
			return fmt.Errorf("replica apply requires --input")
		}
		if !preview && !confirmed {
			return fmt.Errorf("replica apply requires --dry-run or --confirm")
		}
		roots, err := locations()
		if err != nil {
			return err
		}
		items, err := replica.Apply(input, roots, preview)
		label := "Restored"
		if preview {
			label = "Would restore"
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", label, item)
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Selected-item operation completed. Different existing items are never overwritten.")
		return nil
	}}
	apply.Flags().StringVar(&input, "input", "", "Snapshot directory")
	apply.Flags().BoolVar(&preview, "dry-run", false, "Validate and preview without writes")
	apply.Flags().BoolVar(&confirmed, "confirm", false, "Restore selected items")
	verify := &cobra.Command{Use: "verify", Short: "Check selected-item contents and recorded metadata", RunE: func(cmd *cobra.Command, args []string) error {
		if input == "" {
			return fmt.Errorf("replica verify requires --input")
		}
		roots, err := locations()
		if err != nil {
			return err
		}
		result, err := replica.Verify(input, roots)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return err
		}
		if len(result.Differences) > 0 {
			return fmt.Errorf("replica verification found differences")
		}
		return nil
	}}
	verify.Flags().StringVar(&input, "input", "", "Snapshot directory")
	command.AddCommand(capture, apply, verify)
	RootCmd.AddCommand(command)
}
