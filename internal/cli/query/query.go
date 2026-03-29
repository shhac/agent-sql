package query

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
	"github.com/shhac/agent-sql/internal/truncation"
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
  --format json|yaml|csv      Output format (default: jsonl, or config defaults.format)
  --limit <n>                 Max rows (run: from config, sample: 5)
  --write                     Enable write mode (requires write-enabled credential)
  --where <condition>         WHERE clause for sample/count
  --analyze                   EXPLAIN ANALYZE (explain only, read-only queries)
  --expand <fields>           Comma-separated fields to show untruncated
  --full                      Show all fields untruncated

OUTPUT FORMAT (default NDJSON)
  Each row: {"col": val, ..., "@truncated": null}
  Last line when more rows: {"@pagination": {"hasMore": true, "rowCount": 20}}

WRITE OUTPUT
  {"result": "ok", "rowsAffected": 5, "command": "UPDATE"}

SAFETY
  Queries are read-only by default. --write requires a credential with writePermission.
  Long strings are truncated; use --full or --expand to see full values.
`

var sqlHasLimit = regexp.MustCompile(`(?i)\bLIMIT\s+\d+`)

var writePattern = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|TRUNCATE)\b`)

// Register adds the query command group to root.
func Register(root *cobra.Command, globals func() *shared.GlobalFlags) {
	query := &cobra.Command{
		Use:   "query",
		Short: "Run and inspect SQL queries",
	}

	registerRun(query, globals)
	registerSample(query, globals)
	registerExplain(query, globals)
	registerCount(query, globals)

	query.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Show LLM-optimized query command reference",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(usageText)
		},
	})

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
			ctx, cancel := shared.MakeContext(g.Timeout)
			defer cancel()
			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: g.Connection, Write: write, Timeout: g.Timeout})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			return ExecuteRun(ctx, drv, args[0], limit, write, g.Expand, g.Full, g.Compact, g.Format)
		},
	}
	run.Flags().IntVarP(&limit, "limit", "l", 0, "Maximum rows to return")
	run.Flags().BoolVar(&write, "write", false, "Enable write mode")
	parent.AddCommand(run)
}

// ExecuteRun runs a SQL query on an already-resolved connection and writes results.
// Uses streaming (QueryStream) when the driver supports it, falling back to buffered Query.
func ExecuteRun(ctx context.Context, drv driver.Connection, sql string, limitFlag int, write bool, expand string, full bool, compact bool, formatFlag string) error {
	pageSize := resolveLimit(limitFlag)
	maxRows := resolveMaxRows()
	effectiveLimit := pageSize
	if maxRows > 0 && maxRows < effectiveLimit {
		effectiveLimit = maxRows
	}

	isSelectLike := !write && driver.DetectCommand(sql, driver.WriteCommands) == ""
	effectiveSQL := sql
	if isSelectLike && !sqlHasLimit.MatchString(sql) {
		effectiveSQL = strings.TrimRight(strings.TrimRight(sql, " \t\n"), ";") + fmt.Sprintf(" LIMIT %d", effectiveLimit+1)
	}

	opts := driver.QueryOpts{Write: write}
	format := output.ResolveFormat(formatFlag)

	// Try streaming path
	if streamer, ok := drv.(driver.StreamingQuerier); ok && isSelectLike {
		return executeStreaming(ctx, streamer, effectiveSQL, opts, effectiveLimit, expand, full, compact, format)
	}

	// Buffered fallback
	return executeBuffered(ctx, drv, effectiveSQL, opts, write, effectiveLimit, expand, full, compact, format)
}

func executeStreaming(ctx context.Context, streamer driver.StreamingQuerier, sql string, opts driver.QueryOpts, limit int, expand string, full bool, compact bool, format output.Format) error {
	sr, err := streamer.QueryStream(ctx, sql, opts)
	if err != nil {
		output.WriteError(os.Stderr, err)
		return nil
	}
	if sr.Iterator == nil {
		// Write result
		output.PrintJSON(map[string]any{
			"result": "ok", "rowsAffected": sr.RowsAffected, "command": sr.Command,
		}, true)
		return nil
	}
	defer sr.Iterator.Close()

	w := makeWriter(expand, full, compact, format, sr.Iterator.Columns())

	count := 0
	for sr.Iterator.Next() {
		if count >= limit {
			// One extra row means hasMore=true
			w.WritePagination(&output.Pagination{HasMore: true, RowCount: limit})
			w.Flush()
			return nil
		}
		row, err := sr.Iterator.Scan()
		if err != nil {
			output.WriteError(os.Stderr, err)
			return nil
		}
		w.WriteRow(row)
		count++
	}
	if err := sr.Iterator.Err(); err != nil {
		output.WriteError(os.Stderr, err)
		return nil
	}
	w.Flush()
	return nil
}

func executeBuffered(ctx context.Context, drv driver.Connection, sql string, opts driver.QueryOpts, write bool, limit int, expand string, full bool, compact bool, format output.Format) error {
	result, err := drv.Query(ctx, sql, opts)
	if err != nil {
		output.WriteError(os.Stderr, err)
		return nil
	}

	if write && isWriteResult(result) {
		output.PrintJSON(map[string]any{
			"result": "ok", "rowsAffected": result.RowsAffected, "command": result.Command,
		}, true)
		return nil
	}

	hasMore := !write && len(result.Rows) > limit
	displayRows := result.Rows
	if hasMore {
		displayRows = result.Rows[:limit]
	}

	writeQueryResults(displayRows, hasMore, expand, full, compact, format, result.Columns)
	return nil
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

			ctx, cancel := shared.MakeContext(g.Timeout)
			defer cancel()
			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: g.Connection, Timeout: g.Timeout})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			effectiveLimit := limit
			if effectiveLimit <= 0 {
				effectiveLimit = 5
			}

			quoted := drv.QuoteIdent(args[0])
			whereClause := ""
			if where != "" {
				whereClause = " WHERE " + where
			}
			sql := fmt.Sprintf("SELECT * FROM %s%s LIMIT %d", quoted, whereClause, effectiveLimit)

			result, err := drv.Query(ctx, sql, driver.QueryOpts{})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			writeQueryResults(result.Rows, false, g.Expand, g.Full, g.Compact, output.ResolveFormat(g.Format), result.Columns)
			return nil
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
					output.WriteError(os.Stderr, fmt.Errorf(
						"EXPLAIN ANALYZE is not allowed for write queries (detected %s). EXPLAIN ANALYZE actually executes the query, which would modify data. Use EXPLAIN without --analyze for write queries.",
						strings.ToUpper(m[1]),
					))
					return nil
				}
			}

			prefix := "EXPLAIN"
			if analyze {
				prefix = "EXPLAIN ANALYZE"
			}
			sql := prefix + " " + args[0]

			ctx, cancel := shared.MakeContext(g.Timeout)
			defer cancel()
			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: g.Connection, Timeout: g.Timeout})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			result, err := drv.Query(ctx, sql, driver.QueryOpts{})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			output.PrintJSON(map[string]any{"plan": result.Rows}, true)
			return nil
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

			ctx, cancel := shared.MakeContext(g.Timeout)
			defer cancel()
			drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: g.Connection, Timeout: g.Timeout})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			quoted := drv.QuoteIdent(args[0])
			whereClause := ""
			if where != "" {
				whereClause = " WHERE " + where
			}
			sql := fmt.Sprintf("SELECT COUNT(*) AS count FROM %s%s", quoted, whereClause)

			result, err := drv.Query(ctx, sql, driver.QueryOpts{})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			countVal := 0
			if len(result.Rows) > 0 {
				if v, ok := result.Rows[0]["count"]; ok {
					switch n := v.(type) {
					case int64:
						countVal = int(n)
					case float64:
						countVal = int(n)
					case int:
						countVal = n
					}
				}
			}

			output.PrintJSON(map[string]any{"table": args[0], "count": countVal}, true)
			return nil
		},
	}
	count.Flags().StringVar(&where, "where", "", "WHERE clause filter")
	parent.AddCommand(count)
}

// helpers

func resolveLimit(flagLimit int) int {
	if flagLimit > 0 {
		return flagLimit
	}
	cfg := config.Read()
	if cfg.Settings.Defaults != nil && cfg.Settings.Defaults.Limit != nil {
		return *cfg.Settings.Defaults.Limit
	}
	return 20
}

func resolveMaxRows() int {
	cfg := config.Read()
	if cfg.Settings.Query != nil && cfg.Settings.Query.MaxRows != nil {
		return *cfg.Settings.Query.MaxRows
	}
	return 10000
}

func isWriteResult(result *driver.QueryResult) bool {
	switch result.Command {
	case "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE":
		return true
	}
	return false
}

func makeWriter(expand string, full bool, compact bool, format output.Format, columns []string) *truncation.TruncatingWriter {
	expandMap := make(map[string]bool)
	if expand != "" {
		for _, f := range strings.Split(expand, ",") {
			expandMap[strings.TrimSpace(f)] = true
		}
	}

	maxLen := truncation.DefaultMaxLength
	cfg := config.Read()
	if cfg.Settings.Truncation != nil && cfg.Settings.Truncation.MaxLength != nil {
		maxLen = *cfg.Settings.Truncation.MaxLength
	}

	var inner output.ResultWriter
	if compact {
		inner = output.NewCompactWriter(os.Stdout, columns)
	} else {
		inner = output.NewWriter(os.Stdout, format, columns)
	}

	return truncation.NewTruncatingWriter(
		inner,
		truncation.Config{MaxLength: maxLen, Expand: expandMap, Full: full},
	)
}

func writeQueryResults(rows []map[string]any, hasMore bool, expand string, full bool, compact bool, format output.Format, columns []string) {
	w := makeWriter(expand, full, compact, format, columns)

	for _, row := range rows {
		w.WriteRow(row)
	}

	if hasMore {
		w.WritePagination(&output.Pagination{
			HasMore:  true,
			RowCount: len(rows),
		})
	}

	w.Flush()
}
