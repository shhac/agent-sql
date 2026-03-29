package output

import (
	"encoding/json"
	"io"
)

// NDJSONWriter writes query results as NDJSON (one JSON object per line).
type NDJSONWriter struct {
	w   io.Writer
	enc *json.Encoder
}

// NewNDJSONWriter creates a new NDJSON result writer.
func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &NDJSONWriter{w: w, enc: enc}
}

func (n *NDJSONWriter) WriteRow(row map[string]any) error {
	return n.enc.Encode(row)
}

func (n *NDJSONWriter) WritePagination(p *Pagination) error {
	return n.enc.Encode(map[string]any{
		"@pagination": p,
	})
}

func (n *NDJSONWriter) Flush() error {
	return nil
}
