package output

import (
	"io"

	out "github.com/shhac/lib-agent-output"
)

// typedMsg is the compact typed-NDJSON wire shape {"type": ..., "values": ...}
// shared by query --compact (columns/row/pagination lines) and schema
// --compact (one line per schema command). It is the single definition of
// that contract; write it via WriteTypedLine or an NDJSONWriter.
type typedMsg struct {
	Type   string `json:"type"`
	Values any    `json:"values"`
}

// WriteTypedLine writes one {"type": typ, "values": values} NDJSON line to w
// through the shared funnel (colorized on a terminal).
func WriteTypedLine(w io.Writer, typ string, values any) error {
	return out.NewNDJSONWriter(w).WriteItem(typedMsg{Type: typ, Values: values})
}

// CompactWriter writes query results as typed NDJSON messages:
//
//	{"type":"columns","values":["id","name"]}
//	{"type":"row","values":[1,"Alice"]}
//	{"type":"row","values":[2,"Bob"]}
//	{"type":"pagination","values":{"has_more":true,"row_count":2}}
//
// Each line is self-describing and independently parseable.
// Saves tokens by not repeating column names in every row.
type CompactWriter struct {
	nw      *out.NDJSONWriter
	columns []string
	wrote   bool
}

// NewCompactWriter creates a compact result writer.
func NewCompactWriter(w io.Writer, columns []string) *CompactWriter {
	return &CompactWriter{nw: out.NewNDJSONWriter(w), columns: columns}
}

func (c *CompactWriter) WriteRow(row map[string]any) error {
	if !c.wrote {
		if len(c.columns) == 0 {
			c.columns = extractColumns(row)
		} else {
			c.columns = withoutTruncated(c.columns)
		}
		if err := c.nw.WriteItem(typedMsg{Type: "columns", Values: c.columns}); err != nil {
			return err
		}
		c.wrote = true
	}

	arr := make([]any, len(c.columns))
	for i, col := range c.columns {
		arr[i] = row[col]
	}
	return c.nw.WriteItem(typedMsg{Type: "row", Values: arr})
}

func (c *CompactWriter) WritePagination(p *Pagination) error {
	return c.nw.WriteItem(typedMsg{Type: "pagination", Values: p})
}

func (c *CompactWriter) Flush() error {
	return nil
}

// withoutTruncated strips the @truncated meta key from a caller-provided
// column list; extractColumns applies the same rule when deriving columns
// from a row.
func withoutTruncated(cols []string) []string {
	dataCols := make([]string, 0, len(cols))
	for _, col := range cols {
		if col != "@truncated" {
			dataCols = append(dataCols, col)
		}
	}
	return dataCols
}
