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
