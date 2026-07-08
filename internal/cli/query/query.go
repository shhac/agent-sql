package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
	libcli "github.com/shhac/lib-agent-cli/cli"
)

const usageText = `QUERY COMMANDS
==============

Run SQL:
  agent-sql run "<sql>"                    Execute any SQL query
  agent-sql query run "<sql>"              Same as above (long form)
  agent-sql query run "<sql>" --write      Enable write mode (INSERT/UPDATE/DELETE)
  agent-sql query run "<sql>" --compact    Typed NDJSON (columns once, row arrays)
  agent-sql query run "<sql>" --limit 50   Limit result rows

Sample rows:
  agent-sql query sample <table>           Get 5 sample rows
  agent-sql query sample <table> --limit 10
  agent-sql query sample users --where "active = true"
  agent-sql query sample analytics.events  PG namespace (schema.table)

Explain query plan:
  agent-sql query explain "<sql>"          Show execution plan
  agent-sql query explain "<sql>" --analyze  Run EXPLAIN ANALYZE (read-only queries only)

Count rows:
  agent-sql query count <table>            Count all rows
  agent-sql query count users --where "created_at > '2024-01-01'"

OPTIONS
  -c, --connection <alias>    Connection alias, file path, or URL (default: configured default)
  --format json|yaml|csv      Output format (default: jsonl, or config query.format > defaults.format)
  --limit <n>                 Max rows (run: from config, sample: 5)
  --write                     Enable write mode (requires write-enabled credential)
  --where <condition>         WHERE clause for sample/count
  --analyze                   EXPLAIN ANALYZE (explain only, read-only queries)
  --expand <fields>           Comma-separated fields to show untruncated
  --full                      Show all fields untruncated

OUTPUT FORMAT (default NDJSON)
  Each row: {"col": val, ..., "@truncated": null}
  Last line when more rows: {"@pagination": {"has_more": true, "row_count": 20, "hint": "..."}}

WRITE OUTPUT
  {"result": "ok", "rows_affected": 5, "command": "UPDATE"}

SAFETY
  Queries are read-only by default. --write requires a credential with writePermission.
  Long strings are truncated; use --full or --expand to see full values.

PAGINATION
  agent-sql never modifies user SQL. When a SELECT exceeds the row cap (default
  --limit, or your explicit --limit), the cursor is closed early and a final
  @pagination line reports has_more=true. The CLI does not navigate to the next
  page itself — re-run with a larger --limit, or write your own LIMIT/TOP and
  cursor predicate (e.g. WHERE id > <last>) for true pagination.
`

var writePattern = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|TRUNCATE)\b`)

// Register adds the query command group to root.
func Register(root *cobra.Command, globals func() *shared.GlobalFlags) {
	query := &cobra.Command{
		Use:   "query",
		Short: "Run and inspect SQL queries",
	}
	libcli.HandleUnknownCommand(query, "run 'agent-sql query usage' to see the available commands")

	registerRun(query, globals)
	registerSample(query, globals)
	registerExplain(query, globals)
	registerCount(query, globals)

	shared.RegisterUsage(query, "query", usageText)

	root.AddCommand(query)
}

func registerRun(parent *cobra.Command, globals func() *shared.GlobalFlags) {
	var limit int
	var write bool

	run := &cobra.Command{
		Use:   `run "<sql>"`,
		Short: "Execute a SQL query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			ctx, cancel := shared.MakeContext(g.TimeoutMS)
			defer cancel()
			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: g.Connection, Write: write, Timeout: g.TimeoutMS})
			if err != nil {
				return err
			}
			defer func() { _ = drv.Close() }()

			return ExecuteRun(ctx, drv, args[0], limit, write, RenderOpts{
				Expand:     g.Expand,
				Full:       g.Full,
				Compact:    g.Compact,
				FormatFlag: g.Format,
				Connection: g.Connection,
				Debug:      g.Debug,
			})
		},
	}
	run.Flags().IntVarP(&limit, "limit", "l", 0, "Maximum rows to return")
	run.Flags().BoolVar(&write, "write", false, "Enable write mode")
	parent.AddCommand(run)
}

func registerSample(parent *cobra.Command, globals func() *shared.GlobalFlags) {
	var limit int
	var where string

	sample := &cobra.Command{
		Use:   "sample <table>",
		Short: "Return sample rows from a table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				effectiveLimit := limit
				if effectiveLimit <= 0 {
					effectiveLimit = 5
				}

				quoted := drv.QuoteIdent(args[0])
				whereClause := ""
				if where != "" {
					whereClause = " WHERE " + where
				}
				sql := drv.BuildSampleSelect(quoted, whereClause, effectiveLimit)

				result, err := drv.Query(ctx, sql, driver.QueryOpts{})
				if err != nil {
					return err
				}

				writeQueryResults(result.Rows, false, "", RenderOpts{
					Expand:  g.Expand,
					Full:    g.Full,
					Compact: g.Compact,
					format:  output.ResolveFormat(g.Format),
				}, result.Columns)
				return nil
			})
		},
	}
	sample.Flags().IntVar(&limit, "limit", 0, "Number of sample rows (default 5)")
	sample.Flags().StringVar(&where, "where", "", "WHERE clause filter")
	parent.AddCommand(sample)
}

func registerExplain(parent *cobra.Command, globals func() *shared.GlobalFlags) {
	var analyze bool

	explain := &cobra.Command{
		Use:   `explain "<sql>"`,
		Short: "Show the execution plan for a SQL query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()

			if analyze {
				if m := writePattern.FindStringSubmatch(args[0]); len(m) > 1 {
					err := fmt.Errorf(
						"EXPLAIN ANALYZE is not allowed for write queries (detected %s); EXPLAIN ANALYZE actually executes the query, which would modify data. Use EXPLAIN without --analyze for write queries",
						strings.ToUpper(m[1]),
					)
					return err
				}
			}

			prefix := "EXPLAIN"
			if analyze {
				prefix = "EXPLAIN ANALYZE"
			}
			sql := prefix + " " + args[0]

			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				result, err := drv.Query(ctx, sql, driver.QueryOpts{})
				if err != nil {
					return err
				}
				output.PrintResult(g.Format, map[string]any{"plan": result.Rows}, true)
				return nil
			})
		},
	}
	explain.Flags().BoolVar(&analyze, "analyze", false, "Run EXPLAIN ANALYZE (read-only queries only)")
	parent.AddCommand(explain)
}

func registerCount(parent *cobra.Command, globals func() *shared.GlobalFlags) {
	var where string

	count := &cobra.Command{
		Use:   "count <table>",
		Short: "Count rows in a table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				quoted := drv.QuoteIdent(args[0])
				whereClause := ""
				if where != "" {
					whereClause = " WHERE " + where
				}
				sql := fmt.Sprintf("SELECT COUNT(*) AS count FROM %s%s", quoted, whereClause)

				result, err := drv.Query(ctx, sql, driver.QueryOpts{})
				if err != nil {
					return err
				}

				countVal := 0
				if len(result.Rows) > 0 {
					countVal = coerceCount(result.Rows[0]["count"])
				}

				output.PrintResult(g.Format, map[string]any{"table": args[0], "count": countVal}, true)
				return nil
			})
		},
	}
	count.Flags().StringVar(&where, "where", "", "WHERE clause filter")
	parent.AddCommand(count)
}

// coerceCount converts a driver's COUNT(*) value to int. Drivers disagree on
// the Go type a count scans into (int64 for most, float64 after a JSON
// round-trip, plain int from some subprocess drivers); anything else counts
// as zero.
func coerceCount(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
