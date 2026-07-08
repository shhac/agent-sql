package output

import (
	"io"

	out "github.com/shhac/lib-agent-output"
)

// envelopeWriter buffers all rows and writes a {columns, rows, pagination}
// envelope on Flush. JSON and YAML share the buffering and envelope shape;
// only the terminal encode differs. JSON encodes the envelope directly, so
// Pagination keys emit in struct order; YAML must round-trip through JSON
// first so yaml.v3 honors the snake_case json tags (see PrintYAMLViaJSON),
// which sorts its keys. Keep the encode format-aware — merging the two paths
// naively would reorder JSON's pagination keys.
type envelopeWriter struct {
	w          io.Writer
	format     Format
	columns    []string
	rows       []map[string]any
	pagination *Pagination
}

// NewJSONWriter creates a buffered writer that emits a pretty JSON envelope
// on Flush. If columns is non-nil, it defines the column order; otherwise
// columns are extracted from the first row.
func NewJSONWriter(w io.Writer, columns []string) ResultWriter {
	return &envelopeWriter{w: w, format: FormatJSON, columns: columns}
}

// NewYAMLWriter creates a buffered writer that emits a YAML envelope on
// Flush. Column handling matches NewJSONWriter.
func NewYAMLWriter(w io.Writer, columns []string) ResultWriter {
	return &envelopeWriter{w: w, format: FormatYAML, columns: columns}
}

func (e *envelopeWriter) WriteRow(row map[string]any) error {
	if e.columns == nil {
		e.columns = extractColumns(row)
	}
	e.rows = append(e.rows, row)
	return nil
}

func (e *envelopeWriter) WritePagination(p *Pagination) error {
	e.pagination = p
	return nil
}

func (e *envelopeWriter) Flush() error {
	envelope := map[string]any{
		"columns": e.columns,
		"rows":    e.rows,
	}
	if e.pagination != nil {
		envelope["pagination"] = e.pagination
	}
	// Ensure rows/columns are [] not null when empty.
	if e.rows == nil {
		envelope["rows"] = []map[string]any{}
	}
	if e.columns == nil {
		envelope["columns"] = []string{}
	}
	if e.format == FormatYAML {
		return PrintYAMLViaJSON(e.w, envelope)
	}
	return out.PrintJSON(e.w, envelope, nil)
}

// extractColumns pulls column names from a row map in a stable order.
// Excludes internal metadata keys like @truncated.
func extractColumns(row map[string]any) []string {
	cols := make([]string, 0, len(row))
	for k := range row {
		if k == "@truncated" {
			continue
		}
		cols = append(cols, k)
	}
	return cols
}
