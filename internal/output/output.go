// Package output handles formatting and writing query results and errors.
package output

import (
	"io"
	"os"

	out "github.com/shhac/lib-agent-output"
)

// Pagination holds pagination metadata for result sets. The field names are
// snake_case to match the family wire convention; row_count and hint are
// agent-sql domain fields (SQL row caps, not cursor paging), emitted under the
// shared @pagination meta key.
type Pagination struct {
	HasMore  bool   `json:"has_more"`
	RowCount int    `json:"row_count"`
	Hint     string `json:"hint,omitempty"`
}

// ResultWriter writes query results in a streaming fashion.
type ResultWriter interface {
	WriteRow(row map[string]any) error
	WritePagination(p *Pagination) error
	Flush() error
}

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

// PrintResult writes a single resource or receipt to stdout honoring the
// --format flag value (post-boundary, so config defaults are already folded
// in): one JSON line (jsonl default), pretty JSON, or YAML — all through the
// shared funnel so output colorizes on a terminal. Known array fields are
// coerced to [] before nil-pruning so an empty result renders as an empty
// array rather than disappearing.
func PrintResult(flagFormat string, data any, prune bool) {
	pruner := out.Pruner(fixNilArrays)
	if prune {
		pruner = out.Chain(fixNilArrays, out.PruneNils)
	}
	_ = out.Print(os.Stdout, data, displayFormat(ResolveFormat(flagFormat)), pruner)
}

// PrintList writes list-shaped output honoring the --format flag value:
// NDJSON records (default, one per line) with optional @-meta trailer lines,
// or a {"data": [...]} envelope for json/yaml — the family list contract.
func PrintList(flagFormat string, items []any, meta map[string]any, prune bool) {
	var pruner out.Pruner
	if prune {
		pruner = out.PruneNils
	}
	_ = out.WriteList(os.Stdout, displayFormat(ResolveFormat(flagFormat)), items, meta, pruner)
}

// arrayFields are the output-map keys whose values are always arrays. After a
// JSON round-trip a nil slice and a nil scalar both decode as nil, so these
// keys are recognized by name and coerced to [] instead of being pruned.
// Keep this list in sync with any new array fields added to output maps.
var arrayFields = map[string]bool{
	"tables":      true,
	"columns":     true,
	"indexes":     true,
	"constraints": true,
}

// fixNilArrays walks a JSON-decoded tree and replaces nil values under known
// array keys with empty arrays — at any depth and every occurrence, so
// multi-table shapes like schema dump normalize all their tables. It runs as
// a Pruner inside out.Print, which hands it the already-decoded tree.
func fixNilArrays(v any) any {
	switch val := v.(type) {
	case map[string]any:
		fixed := make(map[string]any, len(val))
		for k, child := range val {
			if child == nil && arrayFields[k] {
				fixed[k] = []any{}
				continue
			}
			fixed[k] = fixNilArrays(child)
		}
		return fixed
	case []any:
		fixed := make([]any, len(val))
		for i, child := range val {
			fixed[i] = fixNilArrays(child)
		}
		return fixed
	default:
		return v
	}
}

// Notice writes a non-error advisory to stderr as the family's structured
// {notice, hint} JSON line (out.WriteNotice). Used for cases where an
// operation succeeds but the user should know something happened
// (credentials stripped, fallback used, etc.). Errors are rendered by
// libcli.Run via the family's out.WriteError when RunE returns them.
func Notice(notice, hint string) {
	out.WriteNotice(os.Stderr, notice, hint)
}
