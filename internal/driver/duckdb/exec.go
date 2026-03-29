package duckdb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

func (c *duckdbConn) buildArgs(sql string) []string {
	args := []string{"-cmd", ".mode jsonlines"}
	if c.path != "" && c.readonly {
		args = append(args, "-readonly")
	}
	if c.path != "" {
		args = append(args, c.path)
	}
	args = append(args, "-c", sql)
	return args
}

func (c *duckdbConn) exec(ctx context.Context, sql string) ([]map[string]any, error) {
	args := c.buildArgs(sql)
	cmd := exec.CommandContext(ctx, c.bin, args...)

	stdout, err := cmd.Output()
	stderr := ""
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = string(exitErr.Stderr)
	}

	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "DuckDB query failed"
		}
		return nil, classifyError(msg)
	}

	return parseNDJSON(string(stdout))
}

// execQuery captures both stdout and stderr separately so warnings
// can be forwarded while still detecting errors via exit code.
func (c *duckdbConn) execQuery(ctx context.Context, sql string) ([]map[string]any, error) {
	args := c.buildArgs(sql)
	cmd := exec.CommandContext(ctx, c.bin, args...)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.Output()
	stderrStr := stderrBuf.String()

	if err != nil {
		msg := strings.TrimSpace(stderrStr)
		if msg == "" {
			msg = "DuckDB query failed"
		}
		return nil, classifyError(msg)
	}

	// Forward DuckDB warnings to os.Stderr
	if trimmed := strings.TrimSpace(stderrStr); trimmed != "" {
		fmt.Fprintln(os.Stderr, stderrStr)
	}

	return parseNDJSON(string(stdout))
}

func (c *duckdbConn) execWrite(ctx context.Context, sql string) error {
	args := c.buildArgsWrite(sql)
	cmd := exec.CommandContext(ctx, c.bin, args...)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			msg = "DuckDB query failed"
		}
		return classifyError(msg)
	}
	return nil
}

func (c *duckdbConn) buildArgsWrite(sql string) []string {
	args := []string{"-cmd", ".mode jsonlines"}
	if c.path != "" {
		args = append(args, c.path)
	}
	args = append(args, "-c", sql)
	return args
}

func parseNDJSON(stdout string) ([]map[string]any, error) {
	var results []map[string]any
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines and DuckDB's "{" quirk for empty result sets
		if trimmed == "" || trimmed == "{" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(trimmed), &row); err != nil {
			preview := trimmed
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return nil, errors.New(
				fmt.Sprintf("Failed to parse DuckDB output: %s", preview),
				errors.FixableByAgent,
			)
		}
		results = append(results, row)
	}
	return results, nil
}
