// Package output handles formatting and writing query results and errors.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
	agenterrors "github.com/shhac/agent-sql/internal/errors"
	out "github.com/shhac/lib-agent-output"
)

// Pagination holds pagination metadata for result sets.
type Pagination struct {
	HasMore  bool   `json:"hasMore"`
	RowCount int    `json:"rowCount"`
	Hint     string `json:"hint,omitempty"`
}

// ResultWriter writes query results in a streaming fashion.
type ResultWriter interface {
	WriteRow(row map[string]any) error
	WritePagination(p *Pagination) error
	Flush() error
}

// PrintJSON writes a JSON object to stdout (for admin/schema output).
func PrintJSON(data any, prune bool) {
	// Marshal then fix nil Go slices that serialize as JSON null.
	// After a json round-trip, nil slices and nil scalars both become null,
	// so we can't distinguish them structurally. Instead, we list the known
	// top-level fields that are always arrays and replace their nulls with [].
	// Keep this list in sync with any new array fields added to output maps.
	raw, _ := json.Marshal(data)
	s := string(raw)
	for _, field := range []string{"tables", "columns", "indexes", "constraints", "keys", "connections"} {
		s = strings.Replace(s, `"`+field+`":null`, `"`+field+`":[]`, 1)
	}
	var normalized any
	_ = json.Unmarshal([]byte(s), &normalized)
	if prune {
		normalized = out.PruneNils(normalized)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(normalized)
}

// Warn writes a non-error advisory to stderr with a consistent
// "warning: " prefix and trailing newline. Used for cases where an
// operation succeeds but the user should know something happened
// (credentials stripped, fallback used, etc.). For errors, use
// WriteError, which produces structured JSON; Warn is plain text.
func Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
}

// WriteError writes an error to stderr as JSON.
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
	_ = json.NewEncoder(w).Encode(payload)
}

// Format identifies an output format. The shared formats come from the
// lib-agent-output contract; FormatCSV is an agent-sql-only tabular format the
// shared contract doesn't define, so it stays local.
type Format = out.Format

const (
	FormatNDJSON        = out.FormatNDJSON
	FormatJSON          = out.FormatJSON
	FormatYAML          = out.FormatYAML
	FormatCSV    Format = "csv"
)

// NewWriter creates a ResultWriter for the given format.
func NewWriter(w io.Writer, format Format, columns []string) ResultWriter {
	switch format {
	case FormatJSON:
		return NewJSONWriter(w, columns)
	case FormatYAML:
		return NewYAMLWriter(w, columns)
	case FormatCSV:
		return NewCSVWriter(w, columns)
	default:
		return NewNDJSONWriter(w)
	}
}

// ResolveFormat resolves the output format from flag > config > default.
func ResolveFormat(flagFormat string) Format {
	if flagFormat != "" {
		f, err := ParseFormat(flagFormat)
		if err != nil {
			return FormatNDJSON
		}
		return f
	}
	cfg := config.Read()
	if cfg.Settings.Defaults != nil && cfg.Settings.Defaults.Format != "" {
		f, err := ParseFormat(cfg.Settings.Defaults.Format)
		if err != nil {
			return FormatNDJSON
		}
		return f
	}
	return FormatNDJSON
}

// ParseFormat parses a format string. CSV is agent-sql-only and handled here;
// the shared formats route through the family parser, which is lenient (accepts
// "ndjson"/"yml", case-insensitive, trims spaces) and classifies an unknown
// format as fixable_by:agent. The empty string defaults to NDJSON.
func ParseFormat(s string) (Format, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return FormatNDJSON, nil
	}
	if strings.EqualFold(trimmed, "csv") {
		return FormatCSV, nil
	}
	f, err := out.ParseFormat(s)
	if err != nil {
		return "", fmt.Errorf("unknown format %q, expected: jsonl, json, yaml, csv", strings.TrimSpace(s))
	}
	return f, nil
}
