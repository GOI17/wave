package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"wave/internal/analyzer"
	"wave/internal/executor"
	"wave/internal/migrator"
	"wave/internal/transaction"
	"wave/ui/gui"
	"wave/ui/tui"
)

var (
	outputPath        string
	inputPath         string
	format            string
	dryRun            bool
	confirm           bool
	rollbackConfirm   bool
	quarantineConfirm bool
	uninstallConfirm  bool
	transactionID     string
	startGUI          = gui.StartGUI
	runBrew           = runHomebrew
	// Version is overridden with release build flags.
	Version = "1.1.3"
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
			outputPath = filepath.Join(homeDir, "wave-state.wave")
		}

		// Show progress
		fmt.Println("🔍 Capturing device state...")
		fmt.Printf("   Output: %s\n\n", outputPath)

		analyzer := analyzer.NewMacOSAnalyzer(homeDir)
		executor := executor.NewMacOSExecutor(homeDir, false)
		mig := migrator.NewMigrator(analyzer, executor)

		fmt.Println("📦 Analyzing device...")
		var captureErr error
		if strings.EqualFold(filepath.Ext(outputPath), ".wave") {
			captureErr = mig.CaptureBundle(outputPath, homeDir)
		} else {
			captureErr = mig.Capture(outputPath, format)
		}
		if captureErr != nil {
			return fmt.Errorf("capture failed: %w", captureErr)
		}

		fmt.Println("\n✅ Device state captured successfully!")
		fmt.Printf("📁 State file: %s\n", outputPath)
		fmt.Println("\nNext steps:")
		fmt.Println("  • Keep the archive private; it contains configuration file contents")
		fmt.Println("  • Transfer the .wave archive to the target machine")
		fmt.Println("  • Preview: wave apply --input " + filepath.Base(outputPath) + " --dry-run")
		fmt.Println("  • Apply: wave apply --input " + filepath.Base(outputPath) + " --confirm")

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
			inputPath = filepath.Join(homeDir, "wave-state.wave")
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

		if dryRun {
			if strings.EqualFold(filepath.Ext(inputPath), ".wave") {
				result, err := transaction.Preview(inputPath)
				if err != nil {
					return fmt.Errorf("preview failed: %w", err)
				}
				fmt.Print("\n" + migrator.FormatSummary(result))
				return nil
			}
			return runLegacyPreview(inputPath, homeDir)
		}

		if !strings.EqualFold(filepath.Ext(inputPath), ".wave") {
			return fmt.Errorf("live apply requires a portable .wave archive")
		}
		if !confirm {
			return fmt.Errorf("live apply requires --confirm after reviewing --dry-run output")
		}
		journal, err := transaction.Apply(inputPath, homeDir, filepath.Join(homeDir, ".wave", "transactions"))
		if err != nil {
			return fmt.Errorf("apply failed: %w", err)
		}
		fmt.Print(transaction.FormatApplySummary(journal))
		return nil
	},
}

func runLegacyPreview(path, homeDir string) error {
	analyzer := analyzer.NewMacOSAnalyzer(homeDir)
	executor := executor.NewMacOSExecutor(homeDir, true)
	mig := migrator.NewMigrator(analyzer, executor)
	result, err := mig.Apply(path, true, format)
	if result != nil {
		fmt.Print("\n" + migrator.FormatSummary(result))
	}
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}
	return nil
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback an applied migration transaction",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !rollbackConfirm {
			return fmt.Errorf("rollback requires --confirm; changed items will be preserved as conflicts")
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
		id := transactionID
		if id == "" {
			result, err := transaction.RollbackLatest(homeDir, transactionsDir)
			if err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}
			fmt.Print(transaction.FormatRollbackSummary(result))
			return nil
		}
		result, err := transaction.Rollback(id, homeDir, transactionsDir)
		if err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		fmt.Print(transaction.FormatRollbackSummary(result))
		return nil
	},
}

var transactionsCmd = &cobra.Command{Use: "transactions", Short: "Manage migration transaction metadata"}

var quarantineCmd = &cobra.Command{
	Use:   "quarantine",
	Short: "Move a malformed transaction aside for manual inspection",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !quarantineConfirm || transactionID == "" {
			return fmt.Errorf("quarantine requires --transaction ID and --confirm")
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		destination, err := transaction.Quarantine(transactionID, filepath.Join(homeDir, ".wave", "transactions"))
		if err != nil {
			return err
		}
		fmt.Printf("Transaction quarantined for manual inspection: %s\n", destination)
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

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Wave through Homebrew",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Updating Wave through Homebrew...")
		if err := runBrew("upgrade", "GOI17/wave/wave"); err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Wave through Homebrew",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !uninstallConfirm {
			return fmt.Errorf("uninstall requires --confirm")
		}
		fmt.Println("Uninstalling Wave through Homebrew...")
		if err := runBrew("uninstall", "GOI17/wave/wave"); err != nil {
			return fmt.Errorf("uninstall failed: %w", err)
		}
		return nil
	},
}

func runHomebrew(args ...string) error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return fmt.Errorf("Homebrew is required; install it from https://brew.sh")
	}
	command := exec.Command(brew, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func init() {
	// Global flags
	RootCmd.PersistentFlags().StringVar(&format, "format", "yaml", "Output format: yaml or json")

	// Capture command flags
	captureCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: ~/wave-state.wave)")

	// Apply command flags
	applyCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Input state file path (default: ~/wave-state.wave)")
	applyCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	applyCmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm live apply of a portable .wave archive")
	rollbackCmd.Flags().StringVar(&transactionID, "transaction", "", "Transaction ID (default: latest eligible transaction)")
	rollbackCmd.Flags().BoolVar(&rollbackConfirm, "confirm", false, "Confirm rollback of the selected transaction")
	quarantineCmd.Flags().StringVar(&transactionID, "transaction", "", "Malformed transaction ID")
	quarantineCmd.Flags().BoolVar(&quarantineConfirm, "confirm", false, "Confirm quarantine without deleting transaction data")
	transactionsCmd.AddCommand(quarantineCmd)

	// Verify command flags
	verifyCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Input state file path")

	// GUI command flags
	guiCmd.Flags().String("port", "8080", "Port to run GUI server on")
	uninstallCmd.Flags().BoolVar(&uninstallConfirm, "confirm", false, "Confirm removal of the Wave Homebrew formula")

	// Add commands to root
	RootCmd.AddCommand(captureCmd)
	RootCmd.AddCommand(applyCmd)
	RootCmd.AddCommand(rollbackCmd)
	RootCmd.AddCommand(transactionsCmd)
	RootCmd.AddCommand(verifyCmd)
	RootCmd.AddCommand(diffCmd)
	RootCmd.AddCommand(tuiCmd)
	RootCmd.AddCommand(guiCmd)
	RootCmd.AddCommand(versionCmd)
	RootCmd.AddCommand(updateCmd)
	RootCmd.AddCommand(uninstallCmd)
}
