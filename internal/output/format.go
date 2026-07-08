package output

import (
	"fmt"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
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

// configuredFormat holds the raw --format flag value, set once per invocation
// from the root command's ConfigDefaults hook. Commands that don't thread the
// flag through their signatures (admin/receipt output) resolve it from here.
var configuredFormat string

// ConfigureFormat records the raw --format flag value for CurrentFormat.
func ConfigureFormat(flag string) { configuredFormat = flag }

// CurrentFormat resolves the effective output format (flag > config > NDJSON).
func CurrentFormat() Format { return ResolveFormat(configuredFormat) }

// displayFormat maps the resolved format to one Print/WriteList can render:
// csv is tabular-only, so non-tabular output falls back to the NDJSON default.
func displayFormat(format Format) Format {
	if format == FormatCSV {
		return FormatNDJSON
	}
	return format
}

// ResolveFormat resolves the output format from flag > config > default.
func ResolveFormat(flagFormat string) Format {
	if flagFormat != "" {
		f, err := ParseFormat(flagFormat)
		if err != nil {
			return FormatNDJSON
		}
		return f
	}
	cfg := config.Read()
	if cfg.Settings.Defaults != nil && cfg.Settings.Defaults.Format != "" {
		f, err := ParseFormat(cfg.Settings.Defaults.Format)
		if err != nil {
			return FormatNDJSON
		}
		return f
	}
	return FormatNDJSON
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
