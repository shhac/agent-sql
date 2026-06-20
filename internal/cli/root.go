// Package cli implements the cobra command tree for agent-sql.
package cli

import (
	"os"

	"github.com/spf13/cobra"

	cliconfig "github.com/shhac/agent-sql/internal/cli/config"
	"github.com/shhac/agent-sql/internal/cli/connection"
	clicredential "github.com/shhac/agent-sql/internal/cli/credential"
	"github.com/shhac/agent-sql/internal/cli/query"
	"github.com/shhac/agent-sql/internal/cli/schema"
	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/output"
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

// schemaGlobals returns global flags for schema commands.
func schemaGlobals() schema.SchemaGlobals {
	return schema.SchemaGlobals{
		Connection: flagConnection,
		Timeout:    flagTimeout,
		Format:     flagFormat,
		Compact:    flagCompact,
	}
}

func newRootCmd(version string, reachedRunE *bool) *cobra.Command {
	root := &cobra.Command{
		Use:     "agent-sql",
		Short:   "Read-only-by-default SQL CLI for AI agents",
		Version: version,
		// Silence usage on errors — we print structured JSON errors instead
		SilenceUsage:  true,
		SilenceErrors: true,
		// PersistentPreRun is inherited by every subcommand and runs only after
		// flag parsing and Args validation succeed, just before RunE. RunE error
		// paths render structured JSON themselves; cobra's own usage errors
		// (unknown command/flag, bad args) surface from Execute() before this
		// runs, so the flag lets Execute render those without double-rendering.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			*reachedRunE = true
		},
	}

	root.PersistentFlags().StringVarP(&flagConnection, "connection", "c", "", "Connection alias, file path, or URL")
	root.PersistentFlags().StringVarP(&flagFormat, "format", "f", "", "Output format: jsonl, json, yaml, csv, sql")
	root.PersistentFlags().StringVarP(&flagExpand, "expand", "e", "", "Expand specific truncated fields (comma-separated)")
	root.PersistentFlags().BoolVarP(&flagFull, "full", "F", false, "Expand all truncated fields")
	root.PersistentFlags().IntVarP(&flagTimeout, "timeout", "t", 0, "Query timeout in milliseconds")
	root.PersistentFlags().BoolVarP(&flagCompact, "compact", "C", false, "Compact output (typed NDJSON: columns once, then row arrays)")

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

// Execute runs the CLI. Errors from a command's RunE are already rendered as
// structured JSON by the command itself; cobra's own usage errors (unknown
// command/flag, missing args) are not, so we render those here to honor the
// invariant that no error reaches the user as unstructured text — or silently.
func Execute(version string) error {
	var reachedRunE bool
	err := newRootCmd(version, &reachedRunE).Execute()
	if err != nil && !reachedRunE {
		output.WriteError(os.Stderr, err)
	}
	return err
}
