// Package truncation provides string truncation with metadata tracking.
package truncation

import (
	"fmt"

	"github.com/shhac/agent-sql/internal/output"
)

const DefaultMaxLength = 200

// Config holds truncation settings.
type Config struct {
	MaxLength int
	Expand    map[string]bool // fields to never truncate
	Full      bool            // expand all fields
}

// TruncatingWriter wraps a ResultWriter and applies truncation to string values.
type TruncatingWriter struct {
	inner output.ResultWriter
	cfg   Config
}

// NewTruncatingWriter creates a truncation decorator around a ResultWriter.
func NewTruncatingWriter(inner output.ResultWriter, cfg Config) *TruncatingWriter {
	if cfg.MaxLength <= 0 {
		cfg.MaxLength = DefaultMaxLength
	}
	return &TruncatingWriter{inner: inner, cfg: cfg}
}

func (tw *TruncatingWriter) WriteRow(row map[string]any) error {
	truncated := tw.applyTruncation(row)
	row["@truncated"] = truncated
	return tw.inner.WriteRow(row)
}

func (tw *TruncatingWriter) WritePagination(p *output.Pagination) error {
	return tw.inner.WritePagination(p)
}

func (tw *TruncatingWriter) Flush() error {
	return tw.inner.Flush()
}

func (tw *TruncatingWriter) applyTruncation(row map[string]any) any {
	if tw.cfg.Full {
		return nil
	}

	meta := make(map[string]int)
	for key, val := range row {
		if key == "@truncated" {
			continue
		}
		s, ok := val.(string)
		if !ok || len(s) <= tw.cfg.MaxLength {
			continue
		}
		if tw.cfg.Expand[key] {
			continue
		}

		originalLen := len(s)
		// Truncate at rune boundary
		runes := []rune(s)
		if len(runes) > tw.cfg.MaxLength {
			row[key] = string(runes[:tw.cfg.MaxLength]) + "…"
			meta[key] = originalLen
		}
	}

	if len(meta) == 0 {
		return nil
	}
	// Convert to map[string]any for JSON serialization
	result := make(map[string]any, len(meta))
	for k, v := range meta {
		result[k] = v
	}
	return result
}

// FormatTruncatedLength returns a display string like "500 chars".
func FormatTruncatedLength(n int) string {
	return fmt.Sprintf("%d", n)
}
