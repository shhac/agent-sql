package output

import (
	"io"

	"gopkg.in/yaml.v3"
)

// YAMLWriter buffers all rows and writes YAML on Flush.
type YAMLWriter struct {
	w          io.Writer
	columns    []string
	rows       []map[string]any
	pagination *Pagination
}

// NewYAMLWriter creates a new YAML writer.
// If columns is non-nil, it defines the column order; otherwise columns are
// extracted from the first row.
func NewYAMLWriter(w io.Writer, columns []string) *YAMLWriter {
	return &YAMLWriter{w: w, columns: columns}
}

func (y *YAMLWriter) WriteRow(row map[string]any) error {
	if y.columns == nil {
		y.columns = extractColumns(row)
	}
	y.rows = append(y.rows, row)
	return nil
}

func (y *YAMLWriter) WritePagination(p *Pagination) error {
	y.pagination = p
	return nil
}

func (y *YAMLWriter) Flush() error {
	envelope := map[string]any{
		"columns": y.columns,
		"rows":    y.rows,
	}
	if y.pagination != nil {
		envelope["pagination"] = map[string]any{
			"hasMore":  y.pagination.HasMore,
			"rowCount": y.pagination.RowCount,
		}
	}
	if y.rows == nil {
		envelope["rows"] = []map[string]any{}
	}
	if y.columns == nil {
		envelope["columns"] = []string{}
	}

	enc := yaml.NewEncoder(y.w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(envelope)
}

// PrintYAML writes arbitrary data as YAML to the given writer.
func PrintYAML(w io.Writer, data any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(data)
}
