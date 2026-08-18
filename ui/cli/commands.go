package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"wave/internal/analyzer"
	"wave/internal/executor"
	"wave/internal/migrator"
	"wave/ui/gui"
	"wave/ui/tui"
)

var (
	outputPath string
	inputPath  string
	format     string
	dryRun     bool
	startGUI   = gui.StartGUI
	// Version is overridden with release build flags.
	Version = "1.0.3"
)

// RootCmd is the root command
var RootCmd = &cobra.Command{
	Use:   "wave",
	Short: "Wave – macOS device migrator",
	Long: `Wave is a comprehensive tool to replicate macOS device settings, dotfiles, applications, and configurations.

It supports CLI, TUI, and GUI interfaces for migration workflows.`,
	Version: Version,
}

// captureCmd exports current device state
var captureCmd = &cobra.Command{
	Use:   "capture",
	Short: "Capture device state",
	Long:  `Analyze and export current device configuration including apps, dotfiles, and preferences.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		if outputPath == "" {
			outputPath = filepath.Join(homeDir, "wave-state.yaml")
		}

		// Show progress
		fmt.Println("🔍 Capturing device state...")
		fmt.Printf("   Output: %s\n\n", outputPath)

		analyzer := analyzer.NewMacOSAnalyzer(homeDir)
		executor := executor.NewMacOSExecutor(homeDir, false)
		mig := migrator.NewMigrator(analyzer, executor)

		fmt.Println("📦 Analyzing device...")
		if err := mig.Capture(outputPath, format); err != nil {
			return fmt.Errorf("capture failed: %w", err)
		}

		fmt.Println("\n✅ Device state captured successfully!")
		fmt.Printf("📁 State file: %s\n", outputPath)
		fmt.Println("\nNext steps:")
		fmt.Println("  • Review the captured state: cat " + outputPath)
		fmt.Println("  • Transfer to target machine")
		fmt.Println("  • Run: wave apply --input " + filepath.Base(outputPath) + " --dry-run")
		fmt.Println("  • Then: wave apply --input " + filepath.Base(outputPath))

		return nil
	},
}

// applyCmd applies captured state to target device
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply migration state",
	Long:  `Load and apply a previously captured device state to this device.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if inputPath == "" {
			homeDir, _ := os.UserHomeDir()
			inputPath = filepath.Join(homeDir, "wave-state.yaml")
		}

		// Check if file exists
		if _, err := os.Stat(inputPath); os.IsNotExist(err) {
			return fmt.Errorf("state file not found: %s", inputPath)
		}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		fmt.Println("🌊 Wave Migration Tool")
		fmt.Println("=" + strings.Repeat("=", 38))
		fmt.Printf("\n📁 Input file: %s\n", inputPath)
		if dryRun {
			fmt.Println("🔒 Mode: DRY-RUN (no changes will be applied)")
		} else {
			fmt.Println("⚡ Mode: LIVE (changes will be applied)")
		}

		analyzer := analyzer.NewMacOSAnalyzer(homeDir)
		executor := executor.NewMacOSExecutor(homeDir, dryRun)
		mig := migrator.NewMigrator(analyzer, executor)

		result, err := mig.Apply(inputPath, dryRun, format)
		if result != nil {
			fmt.Print("\n" + migrator.FormatSummary(result))
		}
		if err != nil {
			return fmt.Errorf("apply failed: %w", err)
		}

		return nil
	},
}

// verifyCmd verifies migration success
var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify migration",
	Long:  `Verify that a migration was applied successfully.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("✅ Verification not yet implemented")
		fmt.Println("This will compare current state against captured state")
		return nil
	},
}

// diffCmd shows differences between states
var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show differences",
	Long:  `Compare two migration states and show differences.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("📊 Diff not yet implemented")
		return nil
	},
}

// tuiCmd launches the terminal UI
var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch terminal UI",
	Long:  `Start the interactive terminal user interface for migration workflows.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🌊 Wave TUI")
		return tui.StartTUI(Version)
	},
}

// guiCmd launches the desktop GUI
var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Launch desktop GUI",
	Long:  `Start the web-based graphical interface for migration workflows.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startGUI(cmd.Flag("port").Value.String(), Version)
	},
}

// versionCmd shows version information
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("🌊 Wave v%s\n", Version)
		fmt.Println("macOS Device Migrator")
		fmt.Println("\nFeatures:")
		fmt.Println("  ✓ CLI - Full-featured command line")
		fmt.Println("  ✓ TUI - Interactive terminal UI")
		fmt.Println("  ✓ GUI - Web-based interface")
	},
}

func init() {
	// Global flags
	RootCmd.PersistentFlags().StringVar(&format, "format", "yaml", "Output format: yaml or json")

	// Capture command flags
	captureCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: ~/wave-state.yaml)")

	// Apply command flags
	applyCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Input state file path (default: ~/wave-state.yaml)")
	applyCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")

	// Verify command flags
	verifyCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Input state file path")

	// GUI command flags
	guiCmd.Flags().String("port", "8080", "Port to run GUI server on")

	// Add commands to root
	RootCmd.AddCommand(captureCmd)
	RootCmd.AddCommand(applyCmd)
	RootCmd.AddCommand(verifyCmd)
	RootCmd.AddCommand(diffCmd)
	RootCmd.AddCommand(tuiCmd)
	RootCmd.AddCommand(guiCmd)
	RootCmd.AddCommand(versionCmd)
}
