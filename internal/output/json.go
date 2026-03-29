package output

import (
	"encoding/json"
	"io"
)

// JSONWriter buffers all rows and writes a single JSON envelope on Flush.
type JSONWriter struct {
	w          io.Writer
	columns    []string
	rows       []map[string]any
	pagination *Pagination
}

// NewJSONWriter creates a new JSON envelope writer.
// If columns is non-nil, it defines the column order; otherwise columns are
// extracted from the first row.
func NewJSONWriter(w io.Writer, columns []string) *JSONWriter {
	return &JSONWriter{w: w, columns: columns}
}

func (j *JSONWriter) WriteRow(row map[string]any) error {
	if j.columns == nil {
		j.columns = extractColumns(row)
	}
	j.rows = append(j.rows, row)
	return nil
}

func (j *JSONWriter) WritePagination(p *Pagination) error {
	j.pagination = p
	return nil
}

func (j *JSONWriter) Flush() error {
	envelope := map[string]any{
		"columns": j.columns,
		"rows":    j.rows,
	}
	if j.pagination != nil {
		envelope["pagination"] = j.pagination
	}
	// Ensure rows is [] not null when empty
	if j.rows == nil {
		envelope["rows"] = []map[string]any{}
	}
	if j.columns == nil {
		envelope["columns"] = []string{}
	}
	enc := json.NewEncoder(j.w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(envelope)
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
