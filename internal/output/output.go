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
	json.Unmarshal([]byte(s), &normalized)
	if prune {
		normalized = pruneNulls(normalized)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(normalized)
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
