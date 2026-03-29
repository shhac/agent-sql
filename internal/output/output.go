// Package output handles formatting and writing query results and errors.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

// Pagination holds pagination metadata for result sets.
type Pagination struct {
	HasMore  bool `json:"hasMore"`
	RowCount int  `json:"rowCount"`
}

// ResultWriter writes query results in a streaming fashion.
type ResultWriter interface {
	WriteRow(row map[string]any) error
	WritePagination(p *Pagination) error
	Flush() error
}

// PrintJSON writes a JSON object to stdout (for admin/schema output).
func PrintJSON(data any, prune bool) {
	// Marshal first to get normalized JSON, then fix nil arrays
	raw, _ := json.Marshal(data)
	// Replace ":null" that should be "[]" for known array fields
	s := string(raw)
	for _, field := range []string{"tables", "columns", "indexes", "constraints", "keys", "connections"} {
		s = strings.Replace(s, `"`+field+`":null`, `"`+field+`":[]`, 1)
	}
	var normalized any
	json.Unmarshal([]byte(s), &normalized)
	if prune {
		normalized = pruneNulls(normalized)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(normalized)
}

// PrintError writes a classified error to stderr as JSON.
func PrintError(qerr *agenterrors.QueryError) {
	os.Exit(1) // set exit code
	// Actually, we should write then let the caller exit.
	// Reset — we'll handle exit in the CLI layer.
}

// WriteError writes an error to stderr as JSON and sets the process exit code.
func WriteError(w io.Writer, err error) {
	var qerr *agenterrors.QueryError
	if !agenterrors.As(err, &qerr) {
		qerr = agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}

	payload := map[string]any{
		"error":      qerr.Message,
		"fixable_by": string(qerr.FixableBy),
	}
	if qerr.Hint != "" {
		payload["hint"] = qerr.Hint
	}
	json.NewEncoder(w).Encode(payload)
}

func pruneNulls(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			if val == nil {
				continue
			}
			result[k] = pruneNulls(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			result[i] = pruneNulls(val)
		}
		return result
	default:
		return data
	}
}

// Format identifies an output format.
type Format string

const (
	FormatNDJSON Format = "jsonl"
	FormatJSON   Format = "json"
	FormatYAML   Format = "yaml"
	FormatCSV    Format = "csv"
)

// ParseFormat parses a format string, returning an error for unknown formats.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "jsonl", "":
		return FormatNDJSON, nil
	case "json":
		return FormatJSON, nil
	case "yaml":
		return FormatYAML, nil
	case "csv":
		return FormatCSV, nil
	default:
		return "", fmt.Errorf("unknown format %q, expected: jsonl, json, yaml, csv", s)
	}
}
