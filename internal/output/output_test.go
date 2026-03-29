package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

func TestParseFormatValid(t *testing.T) {
	tests := []struct {
		input string
		want  Format
	}{
		{"jsonl", FormatNDJSON},
		{"", FormatNDJSON},
		{"json", FormatJSON},
		{"yaml", FormatYAML},
		{"csv", FormatCSV},
	}

	for _, tt := range tests {
		t.Run("input="+tt.input, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if err != nil {
				t.Fatalf("ParseFormat(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFormatInvalid(t *testing.T) {
	_, err := ParseFormat("xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Errorf("error should mention the bad format, got: %v", err)
	}
}

func TestWriteErrorWithQueryError(t *testing.T) {
	var buf bytes.Buffer
	qe := agenterrors.New("bad query", agenterrors.FixableByAgent).WithHint("check syntax")
	WriteError(&buf, qe)

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] != "bad query" {
		t.Errorf("error = %v, want %q", payload["error"], "bad query")
	}
	if payload["fixable_by"] != "agent" {
		t.Errorf("fixable_by = %v, want %q", payload["fixable_by"], "agent")
	}
	if payload["hint"] != "check syntax" {
		t.Errorf("hint = %v, want %q", payload["hint"], "check syntax")
	}
}

func TestWriteErrorWithPlainError(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, &plainErr{"connection refused"})

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["error"] != "connection refused" {
		t.Errorf("error = %v, want %q", payload["error"], "connection refused")
	}
	if payload["fixable_by"] != "agent" {
		t.Errorf("fixable_by = %v, want %q", payload["fixable_by"], "agent")
	}
	// No hint for plain errors
	if _, ok := payload["hint"]; ok {
		t.Error("plain error should not have hint")
	}
}

type plainErr struct{ msg string }

func (e *plainErr) Error() string { return e.msg }

func TestPruneNullsRemovesNilValues(t *testing.T) {
	input := map[string]any{
		"name": "Alice",
		"age":  nil,
		"city": "NYC",
	}
	result := pruneNulls(input).(map[string]any)

	if _, ok := result["age"]; ok {
		t.Error("pruneNulls should remove nil values")
	}
	if result["name"] != "Alice" {
		t.Errorf("name = %v, want %q", result["name"], "Alice")
	}
	if result["city"] != "NYC" {
		t.Errorf("city = %v, want %q", result["city"], "NYC")
	}
}

func TestPruneNullsPreservesNonNilValues(t *testing.T) {
	input := map[string]any{
		"a": "hello",
		"b": float64(42),
		"c": true,
		"d": []any{"x", "y"},
	}
	result := pruneNulls(input).(map[string]any)

	if len(result) != 4 {
		t.Errorf("expected 4 keys, got %d", len(result))
	}
}

func TestNDJSONWriterWritesOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewNDJSONWriter(&buf)

	w.WriteRow(map[string]any{"id": float64(1), "name": "Alice"})
	w.WriteRow(map[string]any{"id": float64(2), "name": "Bob"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// --- JSON Writer tests ---

func TestJSONWriterEnvelope(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf, nil)

	w.WriteRow(map[string]any{"id": float64(1), "name": "Alice"})
	w.WriteRow(map[string]any{"id": float64(2), "name": "Bob"})
	w.Flush()

	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}

	cols, ok := envelope["columns"].([]any)
	if !ok {
		t.Fatal("expected columns array")
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 columns, got %d", len(cols))
	}

	rows, ok := envelope["rows"].([]any)
	if !ok {
		t.Fatal("expected rows array")
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	if _, ok := envelope["pagination"]; ok {
		t.Error("pagination should not be present when not set")
	}
}

func TestJSONWriterWithPagination(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf, nil)

	w.WriteRow(map[string]any{"id": float64(1), "name": "Alice"})
	w.WritePagination(&Pagination{HasMore: true, RowCount: 1})
	w.Flush()

	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	pag, ok := envelope["pagination"].(map[string]any)
	if !ok {
		t.Fatal("expected pagination object")
	}
	if pag["hasMore"] != true {
		t.Errorf("hasMore = %v, want true", pag["hasMore"])
	}
	if pag["rowCount"] != float64(1) {
		t.Errorf("rowCount = %v, want 1", pag["rowCount"])
	}
}

func TestJSONWriterEmptyResult(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf, nil)
	w.Flush()

	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	rows, ok := envelope["rows"].([]any)
	if !ok {
		t.Fatal("expected rows array")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// --- YAML Writer tests ---

func TestYAMLWriterOutput(t *testing.T) {
	var buf bytes.Buffer
	w := NewYAMLWriter(&buf, nil)

	w.WriteRow(map[string]any{"id": 1, "name": "Alice"})
	w.WriteRow(map[string]any{"id": 2, "name": "Bob"})
	w.Flush()

	out := buf.String()
	if !strings.Contains(out, "columns:") {
		t.Error("YAML output should contain 'columns:'")
	}
	if !strings.Contains(out, "rows:") {
		t.Error("YAML output should contain 'rows:'")
	}
	if !strings.Contains(out, "Alice") {
		t.Error("YAML output should contain 'Alice'")
	}
	if !strings.Contains(out, "Bob") {
		t.Error("YAML output should contain 'Bob'")
	}
}

func TestYAMLWriterWithPagination(t *testing.T) {
	var buf bytes.Buffer
	w := NewYAMLWriter(&buf, nil)

	w.WriteRow(map[string]any{"id": 1, "name": "Alice"})
	w.WritePagination(&Pagination{HasMore: true, RowCount: 1})
	w.Flush()

	out := buf.String()
	if !strings.Contains(out, "pagination:") {
		t.Errorf("YAML should contain pagination:\n%s", out)
	}
	if !strings.Contains(out, "hasMore: true") {
		t.Errorf("YAML should contain 'hasMore: true':\n%s", out)
	}
}

// --- CSV Writer tests ---

func TestCSVWriterBasic(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"id", "name"})

	w.WriteRow(map[string]any{"id": float64(1), "name": "Alice"})
	w.WriteRow(map[string]any{"id": float64(2), "name": "Bob"})
	w.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d: %q", len(lines), buf.String())
	}
	if lines[0] != "id,name" {
		t.Errorf("header = %q, want %q", lines[0], "id,name")
	}
	if lines[1] != "1,Alice" {
		t.Errorf("row 1 = %q, want %q", lines[1], "1,Alice")
	}
	if lines[2] != "2,Bob" {
		t.Errorf("row 2 = %q, want %q", lines[2], "2,Bob")
	}
}

func TestCSVWriterNulls(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"id", "name"})

	w.WriteRow(map[string]any{"id": float64(1), "name": nil})
	w.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[1] != "1," {
		t.Errorf("null should render as empty: got %q", lines[1])
	}
}

func TestCSVWriterSpecialChars(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"name", "bio"})

	w.WriteRow(map[string]any{"name": "Alice", "bio": "likes commas, quotes\", and\nnewlines"})
	w.Flush()

	lines := buf.String()
	// RFC 4180: fields with commas, quotes, or newlines are quoted
	if !strings.Contains(lines, `"likes commas`) {
		t.Errorf("expected quoted field for special chars:\n%s", lines)
	}
}

func TestCSVWriterPaginationNoop(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"id"})

	w.WriteRow(map[string]any{"id": float64(1)})
	w.WritePagination(&Pagination{HasMore: true, RowCount: 1})
	w.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("CSV should have only header + 1 row (no pagination), got %d lines", len(lines))
	}
}

func TestCSVWriterNestedObject(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf, []string{"id", "data"})

	w.WriteRow(map[string]any{"id": float64(1), "data": map[string]any{"key": "val"}})
	w.Flush()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[1], `{`) {
		t.Errorf("nested object should be JSON string: %q", lines[1])
	}
}

// --- NewWriter factory test ---

func TestNewWriterFactory(t *testing.T) {
	var buf bytes.Buffer

	ndjson := NewWriter(&buf, FormatNDJSON, nil)
	if _, ok := ndjson.(*NDJSONWriter); !ok {
		t.Errorf("FormatNDJSON should return *NDJSONWriter, got %T", ndjson)
	}

	jsonW := NewWriter(&buf, FormatJSON, nil)
	if _, ok := jsonW.(*JSONWriter); !ok {
		t.Errorf("FormatJSON should return *JSONWriter, got %T", jsonW)
	}

	yamlW := NewWriter(&buf, FormatYAML, nil)
	if _, ok := yamlW.(*YAMLWriter); !ok {
		t.Errorf("FormatYAML should return *YAMLWriter, got %T", yamlW)
	}

	csvW := NewWriter(&buf, FormatCSV, []string{"a"})
	if _, ok := csvW.(*CSVWriter); !ok {
		t.Errorf("FormatCSV should return *CSVWriter, got %T", csvW)
	}
}

func TestNDJSONWriterWritePagination(t *testing.T) {
	var buf bytes.Buffer
	w := NewNDJSONWriter(&buf)

	w.WritePagination(&Pagination{HasMore: true, RowCount: 50})

	var obj map[string]any
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	pag, ok := obj["@pagination"].(map[string]any)
	if !ok {
		t.Fatal("expected @pagination key")
	}
	if pag["hasMore"] != true {
		t.Errorf("hasMore = %v, want true", pag["hasMore"])
	}
	if pag["rowCount"] != float64(50) {
		t.Errorf("rowCount = %v, want 50", pag["rowCount"])
	}
}
