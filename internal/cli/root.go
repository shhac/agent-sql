// Package cli implements the cobra command tree for agent-sql.
package cli

import (
	"github.com/spf13/cobra"
)

// Global flags accessible to all commands.
var (
	flagConnection string
	flagFormat     string
	flagExpand     string
	flagFull       bool
	flagTimeout    int
	flagCompact    bool
)

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:     "agent-sql",
		Short:   "Read-only-by-default SQL CLI for AI agents",
		Version: version,
		// Silence usage on errors — we print structured JSON errors instead
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&flagConnection, "connection", "c", "", "Connection alias, file path, or URL")
	root.PersistentFlags().StringVar(&flagFormat, "format", "", "Output format: jsonl, json, yaml, csv")
	root.PersistentFlags().StringVar(&flagExpand, "expand", "", "Expand specific truncated fields (comma-separated)")
	root.PersistentFlags().BoolVar(&flagFull, "full", false, "Expand all truncated fields")
	root.PersistentFlags().IntVar(&flagTimeout, "timeout", 0, "Query timeout in milliseconds")
	root.PersistentFlags().BoolVar(&flagCompact, "compact", false, "Compact output (parallel arrays)")

	// Register command groups
	registerRunCommand(root)
	registerUsageCommand(root)

	return root
}

// Execute runs the CLI.
func Execute(version string) error {
	return newRootCmd(version).Execute()
}
