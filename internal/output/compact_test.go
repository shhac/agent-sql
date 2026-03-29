package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactWriter(t *testing.T) {
	t.Run("writes columns header then row arrays", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"id", "name"})
		w.WriteRow(map[string]any{"id": 1, "name": "Alice"})
		w.WriteRow(map[string]any{"id": 2, "name": "Bob"})
		w.Flush()

		lines := nonEmpty(buf.String())
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d: %s", len(lines), buf.String())
		}

		var header struct {
			Type   string   `json:"type"`
			Values []string `json:"values"`
		}
		json.Unmarshal([]byte(lines[0]), &header)
		if header.Type != "columns" {
			t.Errorf("header type = %s, want columns", header.Type)
		}
		if len(header.Values) != 2 || header.Values[0] != "id" || header.Values[1] != "name" {
			t.Errorf("header values = %v, want [id name]", header.Values)
		}

		var row struct {
			Type   string `json:"type"`
			Values []any  `json:"values"`
		}
		json.Unmarshal([]byte(lines[1]), &row)
		if row.Type != "row" {
			t.Errorf("row type = %s, want row", row.Type)
		}
		if row.Values[1] != "Alice" {
			t.Errorf("row values[1] = %v, want Alice", row.Values[1])
		}
	})

	t.Run("handles multiline strings", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"id", "text"})
		w.WriteRow(map[string]any{"id": 1, "text": "line1\nline2\nline3"})
		w.Flush()

		lines := nonEmpty(buf.String())
		// The multiline value should be in a single JSON line (escaped \n)
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines (header + row), got %d", len(lines))
		}

		var row struct {
			Type   string `json:"type"`
			Values []any  `json:"values"`
		}
		json.Unmarshal([]byte(lines[1]), &row)
		if row.Values[1] != "line1\nline2\nline3" {
			t.Errorf("multiline value = %v", row.Values[1])
		}
	})

	t.Run("handles unicode and emoji", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"text"})
		w.WriteRow(map[string]any{"text": "🎉日本語"})
		w.Flush()

		lines := nonEmpty(buf.String())
		var row struct {
			Values []any `json:"values"`
		}
		json.Unmarshal([]byte(lines[1]), &row)
		if row.Values[0] != "🎉日本語" {
			t.Errorf("unicode value = %v", row.Values[0])
		}
	})

	t.Run("handles NULL values", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"id", "val"})
		w.WriteRow(map[string]any{"id": 1, "val": nil})
		w.Flush()

		lines := nonEmpty(buf.String())
		var row struct {
			Values []any `json:"values"`
		}
		json.Unmarshal([]byte(lines[1]), &row)
		if row.Values[1] != nil {
			t.Errorf("null value = %v, want nil", row.Values[1])
		}
	})

	t.Run("handles empty result", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"id"})
		w.Flush()

		if strings.TrimSpace(buf.String()) != "" {
			t.Errorf("expected empty output, got: %s", buf.String())
		}
	})

	t.Run("writes pagination as typed message", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"id"})
		w.WriteRow(map[string]any{"id": 1})
		w.WritePagination(&Pagination{HasMore: true, RowCount: 1})
		w.Flush()

		lines := nonEmpty(buf.String())
		last := lines[len(lines)-1]
		var msg struct {
			Type   string     `json:"type"`
			Values Pagination `json:"values"`
		}
		json.Unmarshal([]byte(last), &msg)
		if msg.Type != "pagination" {
			t.Errorf("type = %s, want pagination", msg.Type)
		}
		if !msg.Values.HasMore {
			t.Error("hasMore should be true")
		}
	})

	t.Run("handles special characters in values", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewCompactWriter(&buf, []string{"text"})
		w.WriteRow(map[string]any{"text": `has "quotes" and \ backslash and <html>`})
		w.Flush()

		lines := nonEmpty(buf.String())
		var row struct {
			Values []any `json:"values"`
		}
		json.Unmarshal([]byte(lines[1]), &row)
		expected := `has "quotes" and \ backslash and <html>`
		if row.Values[0] != expected {
			t.Errorf("special chars = %v, want %v", row.Values[0], expected)
		}
	})
}

func TestNDJSONMultiline(t *testing.T) {
	var buf bytes.Buffer
	w := NewNDJSONWriter(&buf)
	w.WriteRow(map[string]any{"text": "line1\nline2"})
	w.Flush()

	// Multiline string should produce exactly 1 JSON line (newlines escaped)
	lines := nonEmpty(buf.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %q", len(lines), buf.String())
	}

	var row map[string]any
	json.Unmarshal([]byte(lines[0]), &row)
	if row["text"] != "line1\nline2" {
		t.Errorf("multiline = %v", row["text"])
	}
}

func TestCSVMultiline(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"id", "text"})
	w.WriteRow(map[string]any{"id": 1, "text": "line1\nline2"})
	w.Flush()

	output := buf.String()
	// CSV should quote the multiline value per RFC 4180
	if !strings.Contains(output, "\"line1\nline2\"") {
		t.Errorf("CSV should quote multiline value, got: %q", output)
	}
}

func TestJSONMultiline(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf, []string{"text"})
	w.WriteRow(map[string]any{"text": "line1\nline2"})
	w.Flush()

	var result map[string]any
	json.Unmarshal(buf.Bytes(), &result)
	rows := result["rows"].([]any)
	row := rows[0].(map[string]any)
	if row["text"] != "line1\nline2" {
		t.Errorf("JSON multiline = %v", row["text"])
	}
}

func nonEmpty(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
