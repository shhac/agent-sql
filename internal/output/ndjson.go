package output

import (
	"io"

	out "github.com/shhac/lib-agent-output"
)

// NDJSONWriter writes query results as NDJSON (one JSON object per line). It
// delegates to the shared lib-agent-output writer so each line goes through
// the family funnel (HTML escaping off, per-stream colorization).
type NDJSONWriter struct {
	nw *out.NDJSONWriter
}

// NewNDJSONWriter creates a new NDJSON result writer.
func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	return &NDJSONWriter{nw: out.NewNDJSONWriter(w)}
}

func (n *NDJSONWriter) WriteRow(row map[string]any) error {
	return n.nw.WriteItem(row)
}

func (n *NDJSONWriter) WritePagination(p *Pagination) error {
	return n.nw.WriteMetaLine(out.MetaKeyPagination, p)
}

func (n *NDJSONWriter) Flush() error {
	return nil
}
