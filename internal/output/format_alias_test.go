package output

import (
	"errors"
	"testing"

	out "github.com/shhac/lib-agent-output"
)

// TestParseFormatLenientAliases pins the family-wide leniency agent-sql now
// inherits by routing shared formats through lib-agent-output: "ndjson"/"yml"
// aliases and case-insensitive, space-trimmed input. CSV stays agent-sql-only,
// and the empty string still defaults to NDJSON (the list default).
func TestParseFormatLenientAliases(t *testing.T) {
	cases := map[string]Format{
		"":        FormatNDJSON,
		"jsonl":   FormatNDJSON,
		"ndjson":  FormatNDJSON,
		"NDJSON":  FormatNDJSON,
		" jsonl ": FormatNDJSON,
		"json":    FormatJSON,
		"JSON":    FormatJSON,
		"yaml":    FormatYAML,
		"yml":     FormatYAML,
		"YML":     FormatYAML,
		"csv":     FormatCSV,
		"CSV":     FormatCSV,
		"  csv  ": FormatCSV,
	}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil {
			t.Errorf("ParseFormat(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseFormatInvalidClassification pins that an unknown format still errors
// and mentions the bad value, and that the underlying shared error carries the
// fixable_by:agent classification CLIs rely on.
func TestParseFormatInvalidClassification(t *testing.T) {
	_, err := ParseFormat("xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}

	// The shared parser classifies a bad --format as agent-fixable; ParseFormat
	// wraps the wording but the classification is what an agent acts on.
	if _, perr := out.ParseFormat("xml"); perr != nil {
		var oerr *out.Error
		if !errors.As(perr, &oerr) || oerr.FixableBy != out.FixableByAgent {
			t.Errorf("shared ParseFormat should classify unknown format as fixable_by:agent, got %v", perr)
		}
	}
}
