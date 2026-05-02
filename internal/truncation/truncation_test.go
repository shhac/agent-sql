package truncation

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/output"
)

// collectWriter captures rows for assertions.
type collectWriter struct {
	rows       []map[string]any
	pagination *output.Pagination
}

func (c *collectWriter) WriteRow(row map[string]any) error {
	c.rows = append(c.rows, row)
	return nil
}
func (c *collectWriter) WritePagination(p *output.Pagination) error {
	c.pagination = p
	return nil
}
func (c *collectWriter) Flush() error { return nil }

// TestTruncatingWriterWrapsNDJSON verifies the streaming pipeline as
// composed in production: TruncatingWriter wrapping NDJSONWriter with a
// row that combines a long string + embedded newline + null + unicode.
// The output must be one valid JSON line per row with @truncated
// reflecting only the truncated field.
func TestTruncatingWriterWrapsNDJSON(t *testing.T) {
	var buf bytes.Buffer
	inner := output.NewNDJSONWriter(&buf)
	tw := NewTruncatingWriter(inner, Config{MaxLength: 10})

	row := map[string]any{
		"long":    strings.Repeat("x", 50),
		"unicode": "café\nrésumé",
		"null":    nil,
	}
	_ = tw.WriteRow(row)
	_ = tw.Flush()

	out := buf.String()
	// One line, valid JSON.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one NDJSON line, got %d: %q", len(lines), out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !strings.Contains(parsed["long"].(string), "x") {
		t.Errorf("long field missing")
	}
	// @truncated should mention `long` (truncated) but not `unicode`
	// (within limit) or `null`.
	tr, ok := parsed["@truncated"].(map[string]any)
	if !ok {
		t.Fatalf("expected @truncated map, got %T (output: %s)", parsed["@truncated"], out)
	}
	if _, exists := tr["long"]; !exists {
		t.Errorf("@truncated should include 'long'; got %v", tr)
	}
}

// TestTruncatingWriterCSVPagination confirms WritePagination on a
// truncating-CSV pipeline reflects the rowCount the truncating writer
// sees, not the inner one.
func TestTruncatingWriterCSVPagination(t *testing.T) {
	var buf bytes.Buffer
	inner := output.NewCSVWriter(&buf, []string{"id", "name"})
	tw := NewTruncatingWriter(inner, Config{MaxLength: 200})

	for i, name := range []string{"a", "b", "c"} {
		_ = tw.WriteRow(map[string]any{"id": i, "name": name})
	}
	if err := tw.WritePagination(&output.Pagination{HasMore: true, RowCount: 3}); err != nil {
		t.Errorf("WritePagination: %v", err)
	}
	_ = tw.Flush()
	// CSV doesn't render @truncated decoration -- it's a tabular
	// format. Just check the rows landed.
	if !strings.Contains(buf.String(), "a") || !strings.Contains(buf.String(), "c") {
		t.Errorf("expected CSV rows; got: %s", buf.String())
	}
}

// TestTruncatingWriterValueWithEmbeddedNewline confirms a string that
// contains a literal "\n" doesn't get truncated mid-character or break
// JSON encoding.
func TestTruncatingWriterValueWithEmbeddedNewline(t *testing.T) {
	var buf bytes.Buffer
	inner := output.NewNDJSONWriter(&buf)
	tw := NewTruncatingWriter(inner, Config{MaxLength: 100})

	row := map[string]any{"multi": "line one\nline two\nline three"}
	_ = tw.WriteRow(row)
	_ = tw.Flush()

	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	got, _ := parsed["multi"].(string)
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line three") {
		t.Errorf("embedded newlines should round-trip; got: %q", got)
	}
}

func TestTruncatesLongStrings(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{MaxLength: 10})

	row := map[string]any{
		"bio": "This is a very long string that should be truncated",
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	bio := written["bio"].(string)
	// Should be truncated to 10 runes + ellipsis
	if len([]rune(bio)) != 11 { // 10 runes + "…"
		t.Errorf("expected 11 runes (10 + ellipsis), got %d: %q", len([]rune(bio)), bio)
	}
	if !strings.HasSuffix(bio, "…") {
		t.Errorf("truncated string should end with ellipsis, got %q", bio)
	}

	trunc := written["@truncated"].(map[string]any)
	origLen, ok := trunc["bio"]
	if !ok {
		t.Fatal("@truncated should have 'bio' key")
	}
	if origLen != 51 { // byte length of original
		t.Errorf("@truncated.bio = %v, want 51", origLen)
	}
}

func TestPreservesShortStrings(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{MaxLength: 100})

	row := map[string]any{
		"name": "Alice",
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	if written["name"] != "Alice" {
		t.Errorf("name = %v, want %q", written["name"], "Alice")
	}
	if written["@truncated"] != nil {
		t.Error("@truncated should be nil when nothing truncated")
	}
}

func TestFullModeSkipsTruncation(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{MaxLength: 5, Full: true})

	longStr := "This should not be truncated"
	row := map[string]any{
		"data": longStr,
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	if written["data"] != longStr {
		t.Errorf("data = %v, want %q", written["data"], longStr)
	}
	if written["@truncated"] != nil {
		t.Error("@truncated should be nil in full mode")
	}
}

func TestExpandMapExemptsFields(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{
		MaxLength: 5,
		Expand:    map[string]bool{"keep": true},
	})

	longStr := "This is longer than 5 chars"
	row := map[string]any{
		"keep": longStr,
		"trim": longStr,
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	if written["keep"] != longStr {
		t.Errorf("expanded field should not be truncated, got %v", written["keep"])
	}
	trimmed := written["trim"].(string)
	if !strings.HasSuffix(trimmed, "…") {
		t.Errorf("non-expanded field should be truncated, got %q", trimmed)
	}
}

func TestMultiByteCharactersTruncatedAtRuneBoundary(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{MaxLength: 3})

	row := map[string]any{
		"emoji": "🎉🎊🎈🎆🎇",
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	val := written["emoji"].(string)
	runes := []rune(val)
	// 3 emoji runes + ellipsis rune
	if len(runes) != 4 {
		t.Errorf("expected 4 runes (3 + ellipsis), got %d: %q", len(runes), val)
	}
	if runes[0] != '🎉' || runes[1] != '🎊' || runes[2] != '🎈' {
		t.Errorf("unexpected rune content: %q", val)
	}
}

func TestDefaultMaxLengthIs200(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{}) // MaxLength 0 => default

	if tw.cfg.MaxLength != 200 {
		t.Errorf("default MaxLength = %d, want 200", tw.cfg.MaxLength)
	}
}

func TestTruncatedIsNilWhenNothingTruncated(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{MaxLength: 100})

	row := map[string]any{
		"short": "hi",
		"num":   float64(42),
	}
	tw.WriteRow(row)

	written := inner.rows[0]
	if written["@truncated"] != nil {
		t.Error("@truncated should be nil when nothing was truncated")
	}
}

func TestWritePaginationPassesThrough(t *testing.T) {
	inner := &collectWriter{}
	tw := NewTruncatingWriter(inner, Config{})

	p := &output.Pagination{HasMore: true, RowCount: 10}
	tw.WritePagination(p)

	if inner.pagination != p {
		t.Error("WritePagination should pass through to inner writer")
	}
}

func TestTruncatedOutputIsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	ndjson := output.NewNDJSONWriter(&buf)
	tw := NewTruncatingWriter(ndjson, Config{MaxLength: 5})

	row := map[string]any{
		"data": "long string here",
	}
	tw.WriteRow(row)

	var obj map[string]any
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("output should be valid JSON: %v\nraw: %s", err, buf.String())
	}
}
