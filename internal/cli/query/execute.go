package query

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/truncation"
)

// RenderOpts bundles the row-rendering knobs that travel together from the
// command flags down through the writer pipeline: which fields to show
// untruncated (Expand/Full), whether to use the compact typed-NDJSON writer
// (Compact), and the output Format. FormatFlag is the post-boundary flag
// string (persisted config defaults already folded in by the root's
// ConfigDefaults hook); ExecuteRun parses it into format.
type RenderOpts struct {
	Expand     string
	Full       bool
	Compact    bool
	FormatFlag string
	// Connection is the raw connection string/alias from the flag; used only for
	// debug logging (redacted before write). Empty means "resolved from env/config".
	Connection string
	// Debug, when true, logs the resolved connection and each SQL statement to
	// stderr before execution. Stdout stays clean NDJSON.
	Debug bool

	// format is the resolved output format (flag > config > default), filled
	// in by ExecuteRun before the opts travel down the writer pipeline.
	format output.Format
}

// ExecuteRun runs a SQL query on an already-resolved connection and writes results.
// Uses streaming (QueryStream) when the driver supports it, falling back to buffered Query.
//
// User SQL is sent verbatim — we never modify it to inject LIMIT/TOP. Pagination
// is enforced client-side: we read up to `effectiveLimit+1` rows from the iterator
// and close the cursor early if we hit the cap. The "+1" is the probe for
// has_more=true. Trade-off: the database planner doesn't know we'll stop, so
// huge ORDER BY queries lose LIMIT-aware optimization. Users who care put
// LIMIT/TOP in their own SQL — the hint on the @pagination payload nudges
// them in that direction.
func ExecuteRun(ctx context.Context, drv driver.Connection, sql string, limitFlag int, write bool, render RenderOpts) error {
	if render.Debug {
		debugLog("connection: %s", redactConn(render.Connection))
		debugLog("query: %s", sql)
	}

	pageSize := resolveLimit(limitFlag)
	maxRows := resolveMaxRows()
	effectiveLimit := pageSize
	if maxRows > 0 && maxRows < effectiveLimit {
		effectiveLimit = maxRows
	}
	limitFromUser := limitFlag > 0

	isSelectLike := !write && driver.DetectCommand(sql, driver.WriteCommands) == ""
	opts := driver.QueryOpts{Write: write}
	render.format = output.ResolveFormat(render.FormatFlag)

	// Try streaming path
	if streamer, ok := drv.(driver.StreamingQuerier); ok && isSelectLike {
		return executeStreaming(ctx, streamer, sql, opts, effectiveLimit, limitFromUser, render)
	}

	// Buffered fallback
	return executeBuffered(ctx, drv, sql, opts, write, effectiveLimit, limitFromUser, render)
}

// paginationHint returns guidance for the agent/user when truncation fires.
// The hint differentiates user-supplied --limit (they chose the cap) from
// the implicit default (we picked it for safety) so the suggested action
// is specific.
func paginationHint(limit int, fromUser bool) string {
	source := "default safety cap"
	if fromUser {
		source = "your --limit"
	}
	return fmt.Sprintf(
		"stopped at %s of %d rows; raise --limit for more, or push the cap into your SQL with LIMIT/TOP for planner-side acceleration",
		source, limit,
	)
}

func executeStreaming(ctx context.Context, streamer driver.StreamingQuerier, sql string, opts driver.QueryOpts, limit int, limitFromUser bool, render RenderOpts) error {
	sr, err := streamer.QueryStream(ctx, sql, opts)
	if err != nil {
		return err
	}
	if sr.Iterator == nil {
		// Write result
		output.PrintResult(render.FormatFlag, map[string]any{
			"result": "ok", "rows_affected": sr.RowsAffected, "command": sr.Command,
		}, true)
		return nil
	}
	defer func() { _ = sr.Iterator.Close() }()

	w := makeWriter(render, sr.Iterator.Columns())

	count := 0
	for sr.Iterator.Next() {
		if count >= limit {
			// We pulled one row past the limit — closing the iterator (via the
			// deferred Close above) cancels the cursor server-side on every
			// driver we use, so the database stops streaming further rows.
			_ = w.WritePagination(&output.Pagination{
				HasMore:  true,
				RowCount: limit,
				Hint:     paginationHint(limit, limitFromUser),
			})
			_ = w.Flush()
			return nil
		}
		row, err := sr.Iterator.Scan()
		if err != nil {
			return err
		}
		_ = w.WriteRow(row)
		count++
	}
	if err := sr.Iterator.Err(); err != nil {
		return err
	}
	_ = w.Flush()
	return nil
}

func executeBuffered(ctx context.Context, drv driver.Connection, sql string, opts driver.QueryOpts, write bool, limit int, limitFromUser bool, render RenderOpts) error {
	result, err := drv.Query(ctx, sql, opts)
	if err != nil {
		return err
	}

	if write && isWriteResult(result) {
		output.PrintResult(render.FormatFlag, map[string]any{
			"result": "ok", "rows_affected": result.RowsAffected, "command": result.Command,
		}, true)
		return nil
	}

	hasMore := !write && len(result.Rows) > limit
	displayRows := result.Rows
	hint := ""
	if hasMore {
		displayRows = result.Rows[:limit]
		hint = paginationHint(limit, limitFromUser)
	}

	writeQueryResults(displayRows, hasMore, hint, render, result.Columns)
	return nil
}

// debugLog writes a [debug] line to stderr. Only called when debug mode is on;
// stdout stays clean NDJSON regardless.
func debugLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[debug] "+format+"\n", args...)
}

// redactConn returns the connection string with any embedded password replaced
// by "***". Aliases and file paths pass through unchanged; URLs get their
// userinfo password component cleared.
func redactConn(conn string) string {
	if conn == "" {
		return "(from env/config)"
	}
	u, err := url.Parse(conn)
	if err != nil || u.Scheme == "" {
		// Not a URL — alias or file path; safe to show as-is.
		return conn
	}
	if _, hasPwd := u.User.Password(); hasPwd {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}

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

func makeWriter(render RenderOpts, columns []string) *truncation.TruncatingWriter {
	expandMap := make(map[string]bool)
	if render.Expand != "" {
		for _, f := range strings.Split(render.Expand, ",") {
			expandMap[strings.TrimSpace(f)] = true
		}
	}

	maxLen := truncation.DefaultMaxLength
	cfg := config.Read()
	if cfg.Settings.Truncation != nil && cfg.Settings.Truncation.MaxLength != nil {
		maxLen = *cfg.Settings.Truncation.MaxLength
	}

	var inner output.ResultWriter
	if render.Compact {
		inner = output.NewCompactWriter(os.Stdout, columns)
	} else {
		inner = output.NewWriter(os.Stdout, render.format, columns)
	}

	return truncation.NewTruncatingWriter(
		inner,
		truncation.Config{MaxLength: maxLen, Expand: expandMap, Full: render.Full},
	)
}

func writeQueryResults(rows []map[string]any, hasMore bool, hint string, render RenderOpts, columns []string) {
	w := makeWriter(render, columns)

	for _, row := range rows {
		_ = w.WriteRow(row)
	}

	if hasMore {
		_ = w.WritePagination(&output.Pagination{
			HasMore:  true,
			RowCount: len(rows),
			Hint:     hint,
		})
	}

	_ = w.Flush()
}
