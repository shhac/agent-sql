package output

import (
	"fmt"
	"strings"

	out "github.com/shhac/lib-agent-output"
)

// Format identifies an output format. The shared formats come from the
// lib-agent-output contract; FormatCSV is an agent-sql-only tabular format the
// shared contract doesn't define, so it stays local.
type Format = out.Format

const (
	FormatNDJSON        = out.FormatNDJSON
	FormatJSON          = out.FormatJSON
	FormatYAML          = out.FormatYAML
	FormatCSV    Format = "csv"
)

// displayFormat maps the resolved format to one Print/WriteList can render:
// csv is tabular-only, so non-tabular output on a csv-capable command (e.g.
// `query count --format csv`) falls back to the NDJSON default.
func displayFormat(format Format) Format {
	if format == FormatCSV {
		return FormatNDJSON
	}
	return format
}

// ResolveFormat parses the --format flag value, defaulting to NDJSON. The
// persisted config defaults (query.format / defaults.format) are folded into
// the flag at the root's ConfigDefaults boundary before validation, so this
// is a pure parse — the output layer never reads the config store.
func ResolveFormat(flagFormat string) Format {
	f, err := ParseFormat(flagFormat)
	if err != nil {
		return FormatNDJSON
	}
	return f
}

// ParseFormat parses a format string. CSV is agent-sql-only and handled here;
// the shared formats route through the family parser, which is lenient (accepts
// "ndjson"/"yml", case-insensitive, trims spaces) and classifies an unknown
// format as fixable_by:agent. The empty string defaults to NDJSON.
func ParseFormat(s string) (Format, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return FormatNDJSON, nil
	}
	if strings.EqualFold(trimmed, "csv") {
		return FormatCSV, nil
	}
	f, err := out.ParseFormat(s)
	if err != nil {
		return "", fmt.Errorf("unknown format %q, expected: jsonl, json, yaml, csv", strings.TrimSpace(s))
	}
	return f, nil
}
