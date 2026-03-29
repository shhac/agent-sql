package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
)

// CSVWriter streams rows as CSV. Column order is fixed at construction.
type CSVWriter struct {
	w       *csv.Writer
	columns []string
	wrote   bool
}

// NewCSVWriter creates a new CSV writer with the given column order.
func NewCSVWriter(w io.Writer, columns []string) *CSVWriter {
	return &CSVWriter{
		w:       csv.NewWriter(w),
		columns: columns,
	}
}

func (c *CSVWriter) WriteRow(row map[string]any) error {
	if !c.wrote {
		if err := c.w.Write(c.columns); err != nil {
			return err
		}
		c.wrote = true
	}

	record := make([]string, len(c.columns))
	for i, col := range c.columns {
		record[i] = formatCSVValue(row[col])
	}
	return c.w.Write(record)
}

// WritePagination is a no-op for CSV — no metadata support.
func (c *CSVWriter) WritePagination(_ *Pagination) error {
	return nil
}

func (c *CSVWriter) Flush() error {
	c.w.Flush()
	return c.w.Error()
}

func formatCSVValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case int:
		return fmt.Sprintf("%d", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case map[string]any, []any:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}
