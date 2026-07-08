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

// TestCurrentFormatPrecedence pins the flag > config > NDJSON chain used by
// admin/receipt output that doesn't thread --format through its signatures.
func TestCurrentFormatPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from the user's config
	defer ConfigureFormat("")

	ConfigureFormat("")
	if got := CurrentFormat(); got != FormatNDJSON {
		t.Errorf("no flag, no config: CurrentFormat() = %q, want %q", got, FormatNDJSON)
	}

	ConfigureFormat("yaml")
	if got := CurrentFormat(); got != FormatYAML {
		t.Errorf("flag=yaml: CurrentFormat() = %q, want %q", got, FormatYAML)
	}

	ConfigureFormat("bogus")
	if got := CurrentFormat(); got != FormatNDJSON {
		t.Errorf("invalid flag falls back: CurrentFormat() = %q, want %q", got, FormatNDJSON)
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
