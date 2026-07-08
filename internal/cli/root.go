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
	"github.com/shhac/agent-sql/internal/credential"
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
		ConfigDefaults: func() {
			// Record the raw --format value so admin/receipt output (which
			// doesn't thread the flag through command signatures) resolves
			// the same flag > config > NDJSON chain as query output.
			output.ConfigureFormat(g.Format)
		},
		UnknownHint: "run 'agent-sql usage' to see the available commands",
	})

	// Domain persistent flags. --format/--timeout/--debug/--color are bound by
	// NewRoot; override --format's usage text since agent-sql also supports csv
	// on tabular commands. NewRoot's PersistentPreRunE is kept intact — it
	// resolves --color into the output package and validates --format (with
	// csv opted in per command group via AllowFormats below).
	pf := root.PersistentFlags()
	pf.Lookup("format").Usage = "Output format: jsonl, json, yaml (csv on query commands)"

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

	// csv is an agent-sql-only tabular format: valid on the commands that
	// render rows (query group + top-level run alias), rejected with a
	// structured error everywhere else by NewRoot's --format validator.
	for _, c := range root.Commands() {
		if c.Name() == "query" || c.Name() == "run" {
			libcli.AllowFormats(c, "csv")
		}
	}

	// Expose the whole command tree as an MCP server (added last, so it reflects
	// the complete tree). --color/--expose are output-shaping, irrelevant to a
	// tool call, so hide them from the generated schemas.
	// Opt the agent-facing groups into the MCP tool surface: each becomes one
	// coarse tool that dispatches its subcommands (with a "help" verb), so the
	// surface is ~one-tool-per-group instead of one-per-leaf. Credential/config/
	// usage commands are deliberately left out — they aren't agent tasks.
	exposeGroups(root,
		"query", "run", "schema")

	root.AddCommand(agentmcp.Command(root,
		agentmcp.WithHiddenFlags("color", "expose"),
		agentmcp.WithOAuthKeyringService(credential.MCPKeychainService())))

	return root
}

// Run builds the root command and executes it. libcli.Run renders any bubbled
// error as the family's structured JSON on stderr (exactly once) and exits
// non-zero — including cobra's own usage errors (unknown command/flag, bad
// arg count), which is why agent-sql no longer needs a reachedRunE hack.
func Run(version string) {
	libcli.Run(newRootCmd(version))
}

// exposeGroups opts the named top-level commands into the MCP tool surface.
// A name with no matching command is skipped silently — the list is a curation
// of agent-facing groups, not a registration check.
func exposeGroups(root *cobra.Command, names ...string) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for _, c := range root.Commands() {
		if want[c.Name()] {
			agentmcp.Expose(c)
		}
	}
}
