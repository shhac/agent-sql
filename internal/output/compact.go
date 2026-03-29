package output

import (
	"encoding/json"
	"io"
)

// CompactWriter writes query results as typed NDJSON messages:
//
//	{"type":"columns","values":["id","name"]}
//	{"type":"row","values":[1,"Alice"]}
//	{"type":"row","values":[2,"Bob"]}
//	{"type":"pagination","values":{"hasMore":true,"rowCount":2}}
//
// Each line is self-describing and independently parseable.
// Saves tokens by not repeating column names in every row.
type CompactWriter struct {
	enc     *json.Encoder
	columns []string
	wrote   bool
}

// NewCompactWriter creates a compact result writer.
func NewCompactWriter(w io.Writer, columns []string) *CompactWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &CompactWriter{enc: enc, columns: columns}
}

func (c *CompactWriter) WriteRow(row map[string]any) error {
	if !c.wrote {
		if len(c.columns) == 0 {
			for k := range row {
				if k != "@truncated" {
					c.columns = append(c.columns, k)
				}
			}
		}
		// Filter @truncated from columns header
		dataCols := make([]string, 0, len(c.columns))
		for _, col := range c.columns {
			if col != "@truncated" {
				dataCols = append(dataCols, col)
			}
		}
		c.columns = dataCols
		if err := c.enc.Encode(msg{"columns", c.columns}); err != nil {
			return err
		}
		c.wrote = true
	}

	arr := make([]any, len(c.columns))
	for i, col := range c.columns {
		arr[i] = row[col]
	}
	return c.enc.Encode(msg{"row", arr})
}

func (c *CompactWriter) WritePagination(p *Pagination) error {
	return c.enc.Encode(msg{"pagination", p})
}

func (c *CompactWriter) Flush() error {
	return nil
}

type msg struct {
	Type   string `json:"type"`
	Values any    `json:"values"`
}
