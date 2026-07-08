package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// TestDisplayFormat pins the csv fallback: csv is tabular-only, so
// non-tabular output (receipts, lists) downgrades it to the NDJSON default
// while every universal format passes through untouched.
func TestDisplayFormat(t *testing.T) {
	cases := map[Format]Format{
		FormatCSV:    FormatNDJSON,
		FormatNDJSON: FormatNDJSON,
		FormatJSON:   FormatJSON,
		FormatYAML:   FormatYAML,
	}
	for in, want := range cases {
		if got := displayFormat(in); got != want {
			t.Errorf("displayFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveFormatIsPure pins that the output layer's format resolution is a
// pure parse of the post-boundary flag value: empty → NDJSON, valid values
// parse, and an invalid value (unreachable in production — the root validates
// the flag) falls back to NDJSON rather than erroring. Persisted config
// defaults are folded into the flag at the root's ConfigDefaults hook, so no
// config store is consulted here.
func TestResolveFormatIsPure(t *testing.T) {
	cases := map[string]Format{
		"":      FormatNDJSON,
		"yaml":  FormatYAML,
		"csv":   FormatCSV,
		"bogus": FormatNDJSON,
	}
	for in, want := range cases {
		if got := ResolveFormat(in); got != want {
			t.Errorf("ResolveFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNotice pins the structured stderr advisory contract: one JSON line of
// {notice, hint}, with hint omitted when empty — the family shape that
// replaced the old plain-text "warning:" lines.
func TestNotice(t *testing.T) {
	capture := func(fn func()) string {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		prev := os.Stderr
		os.Stderr = w
		fn()
		_ = w.Close()
		os.Stderr = prev
		data, _ := io.ReadAll(r)
		_ = r.Close()
		return string(data)
	}

	withHint := capture(func() { Notice("stored in plaintext", "use the keychain") })
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace([]byte(withHint)), &payload); err != nil {
		t.Fatalf("notice is not one JSON line: %v\n%s", err, withHint)
	}
	if payload["notice"] != "stored in plaintext" {
		t.Errorf("notice = %v", payload["notice"])
	}
	if payload["hint"] != "use the keychain" {
		t.Errorf("hint = %v", payload["hint"])
	}

	noHint := capture(func() { Notice("plain advisory", "") })
	payload = nil
	if err := json.Unmarshal(bytes.TrimSpace([]byte(noHint)), &payload); err != nil {
		t.Fatalf("notice is not one JSON line: %v\n%s", err, noHint)
	}
	if _, ok := payload["hint"]; ok {
		t.Error("empty hint should be omitted from the notice payload")
	}
}
