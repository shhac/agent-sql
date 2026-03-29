package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	"github.com/shhac/agent-sql/internal/resolve"
	"github.com/shhac/agent-sql/internal/truncation"
)

var runSQLHasLimit = regexp.MustCompile(`(?i)\bLIMIT\s+\d+`)

func registerRunCommand(root *cobra.Command) {
	var (
		limit int
		write bool
	)

	run := &cobra.Command{
		Use:   `run "<sql>"`,
		Short: "Execute a SQL query (top-level alias for query run)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sql := args[0]

			ctx := context.Background()
			if flagTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(flagTimeout)*time.Millisecond)
				defer cancel()
			}

			drv, err := resolve.Resolve(ctx, resolve.Opts{
				Connection: flagConnection,
				Write:      write,
				Timeout:    flagTimeout,
			})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			defer drv.Close()

			pageSize := resolveRunLimit(limit)
			maxRows := resolveRunMaxRows()
			effectiveLimit := pageSize
			if maxRows > 0 && maxRows < effectiveLimit {
				effectiveLimit = maxRows
			}

			effectiveSQL := sql
			if !write && !runSQLHasLimit.MatchString(sql) {
				effectiveSQL = strings.TrimRight(strings.TrimRight(sql, " \t\n"), ";") + fmt.Sprintf(" LIMIT %d", effectiveLimit+1)
			}

			result, err := drv.Query(ctx, effectiveSQL, driver.QueryOpts{Write: write})
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			if write && isRunWriteResult(result) {
				output.PrintJSON(map[string]any{
					"result":       "ok",
					"rowsAffected": result.RowsAffected,
					"command":      result.Command,
				}, true)
				return nil
			}

			hasMore := !write && len(result.Rows) > effectiveLimit
			displayRows := result.Rows
			if hasMore {
				displayRows = result.Rows[:effectiveLimit]
			}

			writeRunResults(displayRows, hasMore)
			return nil
		},
	}
	run.Flags().IntVarP(&limit, "limit", "l", 0, "Maximum rows to return")
	run.Flags().BoolVar(&write, "write", false, "Enable write mode")
	root.AddCommand(run)
}

func resolveRunLimit(flagLimit int) int {
	if flagLimit > 0 {
		return flagLimit
	}
	cfg := config.Read()
	if cfg.Settings.Defaults != nil && cfg.Settings.Defaults.Limit != nil {
		return *cfg.Settings.Defaults.Limit
	}
	return 20
}

func resolveRunMaxRows() int {
	cfg := config.Read()
	if cfg.Settings.Query != nil && cfg.Settings.Query.MaxRows != nil {
		return *cfg.Settings.Query.MaxRows
	}
	return 10000
}

func isRunWriteResult(result *driver.QueryResult) bool {
	switch result.Command {
	case "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE":
		return true
	}
	return false
}

func writeRunResults(rows []map[string]any, hasMore bool) {
	expandMap := make(map[string]bool)
	if flagExpand != "" {
		for _, f := range strings.Split(flagExpand, ",") {
			expandMap[strings.TrimSpace(f)] = true
		}
	}

	maxLen := truncation.DefaultMaxLength
	cfg := config.Read()
	if cfg.Settings.Truncation != nil && cfg.Settings.Truncation.MaxLength != nil {
		maxLen = *cfg.Settings.Truncation.MaxLength
	}

	w := truncation.NewTruncatingWriter(
		output.NewNDJSONWriter(os.Stdout),
		truncation.Config{MaxLength: maxLen, Expand: expandMap, Full: flagFull},
	)

	for _, row := range rows {
		w.WriteRow(row)
	}

	if hasMore {
		w.WritePagination(&output.Pagination{
			HasMore:  true,
			RowCount: len(rows),
		})
	}
}
