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
	agenterrors "github.com/shhac/agent-sql/internal/errors"
	"github.com/shhac/agent-sql/internal/output"
	libcli "github.com/shhac/lib-agent-cli/cli"
	agentmcp "github.com/shhac/lib-agent-mcp"
)

// GlobalFlags carries the persistent flags shared by every command. The
// family-shared --format/--timeout/--debug live in the embedded libcli.Globals;
// the rest are agent-sql domain flags (connection, truncation expansion, output
// shape).
type GlobalFlags struct {
	libcli.Globals // Format, TimeoutMS, Debug

	Connection string
	Expand     string
	Full       bool
	Compact    bool
}

// allGlobals returns all global flag values for query commands.
func (g *GlobalFlags) allGlobals() *shared.GlobalFlags {
	return &shared.GlobalFlags{
		Connection: g.Connection,
		Format:     g.Format,
		Expand:     g.Expand,
		Full:       g.Full,
		TimeoutMS:  g.TimeoutMS,
		Compact:    g.Compact,
		Debug:      g.Debug,
	}
}

// connGlobals returns the connection and timeout global flags for connection commands.
func (g *GlobalFlags) connGlobals() (string, int) {
	return g.Connection, g.TimeoutMS
}

// schemaGlobals returns global flags for schema commands.
func (g *GlobalFlags) schemaGlobals() schema.SchemaGlobals {
	return schema.SchemaGlobals{
		Connection: g.Connection,
		TimeoutMS:  g.TimeoutMS,
		Format:     g.Format,
		Compact:    g.Compact,
	}
}

func newRootCmd(version string) *cobra.Command {
	g := &GlobalFlags{}

	root := libcli.NewRoot(libcli.Options{
		Use:           "agent-sql",
		Short:         "Read-only-by-default SQL CLI for AI agents",
		Version:       version,
		Globals:       &g.Globals,
		DefaultFormat: output.FormatNDJSON,
		UnknownHint:   "run 'agent-sql usage' to see the available commands",
	})

	// Domain persistent flags. --format/--timeout/--debug are bound by NewRoot;
	// override --format's usage text since agent-sql also supports csv.
	pf := root.PersistentFlags()
	pf.Lookup("format").Usage = "Output format: jsonl, json, yaml, csv"

	// Replace NewRoot's --format validation: it uses the family parser, which
	// rejects csv. agent-sql supports csv as a domain format, so validate up
	// front with our own csv-aware parser instead (a bad format still surfaces
	// as a structured fixable_by:agent error, exactly once, via libcli.Run).
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if g.Format != "" {
			if _, err := output.ParseFormat(g.Format); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
		}
		return nil
	}

	pf.StringVarP(&g.Connection, "connection", "c", "", "Connection alias, file path, or URL")
	pf.StringVarP(&g.Expand, "expand", "e", "", "Expand specific truncated fields (comma-separated)")
	pf.BoolVarP(&g.Full, "full", "F", false, "Expand all truncated fields")
	pf.BoolVarP(&g.Compact, "compact", "C", false, "Compact output (typed NDJSON: columns once, then row arrays)")
	// --debug is wired via allGlobals → shared.GlobalFlags.Debug → ExecuteRun,
	// which logs the resolved connection (redacted) and each SQL statement to
	// stderr before execution. Stdout stays clean NDJSON regardless.

	// Register command groups
	registerRunCommand(root, g)
	registerUsageCommand(root)
	query.Register(root, g.allGlobals)
	schema.Register(root, g.schemaGlobals)
	connection.Register(root, g.connGlobals)
	clicredential.Register(root)
	cliconfig.Register(root, g.allGlobals)

	// Expose the whole command tree as an MCP server (added last, so it reflects
	// the complete tree). --color/--expose are output-shaping, irrelevant to a
	// tool call, so hide them from the generated schemas.
	root.AddCommand(agentmcp.Command(root, agentmcp.WithHiddenFlags("color", "expose")))

	return root
}

// Run builds the root command and executes it. libcli.Run renders any bubbled
// error as the family's structured JSON on stderr (exactly once) and exits
// non-zero — including cobra's own usage errors (unknown command/flag, bad
// arg count), which is why agent-sql no longer needs a reachedRunE hack.
func Run(version string) {
	libcli.Run(newRootCmd(version))
}
