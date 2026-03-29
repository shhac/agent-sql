// Package cli implements the cobra command tree for agent-sql.
package cli

import (
	"github.com/spf13/cobra"

	cliconfig "github.com/shhac/agent-sql/internal/cli/config"
	"github.com/shhac/agent-sql/internal/cli/connection"
	clicredential "github.com/shhac/agent-sql/internal/cli/credential"
	"github.com/shhac/agent-sql/internal/cli/query"
	"github.com/shhac/agent-sql/internal/cli/schema"
	"github.com/shhac/agent-sql/internal/cli/shared"
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

// allGlobals returns all global flag values for query commands.
func allGlobals() *shared.GlobalFlags {
	return &shared.GlobalFlags{
		Connection: flagConnection,
		Format:     flagFormat,
		Expand:     flagExpand,
		Full:       flagFull,
		Timeout:    flagTimeout,
		Compact:    flagCompact,
	}
}

// connGlobals returns the connection and timeout global flags for schema/connection commands.
func connGlobals() (string, int) {
	return flagConnection, flagTimeout
}

// schemaGlobals returns connection, timeout, and format for schema commands.
func schemaGlobals() (string, int, string) {
	return flagConnection, flagTimeout, flagFormat
}

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
	root.PersistentFlags().BoolVar(&flagCompact, "compact", false, "Compact output (typed NDJSON: columns once, then row arrays)")

	// Register command groups
	registerRunCommand(root)
	registerUsageCommand(root)
	query.Register(root, allGlobals)
	schema.Register(root, schemaGlobals)
	connection.Register(root, connGlobals)
	clicredential.Register(root)
	cliconfig.Register(root)

	return root
}

// Execute runs the CLI.
func Execute(version string) error {
	return newRootCmd(version).Execute()
}
