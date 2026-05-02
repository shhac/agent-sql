package duckdb

import (
	"sort"
	"strings"
)

// buildOptionsPrelude turns Options into `SET k='v'; ...` (alphabetized).
// The reserved key "extensions" is comma-split into INSTALL+LOAD pairs.
// Single quotes in values are doubled (SQL string escaping).
func buildOptionsPrelude(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		v := opts[k]
		if k == "extensions" {
			writeExtensions(&sb, v)
			continue
		}
		writeSetStmt(&sb, k, v)
	}
	return sb.String()
}

// writeExtensions appends `INSTALL <ext>; LOAD <ext>; ` for each
// comma-separated extension name in csv. Empty entries are skipped.
func writeExtensions(sb *strings.Builder, csv string) {
	for _, ext := range strings.Split(csv, ",") {
		e := strings.TrimSpace(ext)
		if e == "" {
			continue
		}
		sb.WriteString("INSTALL ")
		sb.WriteString(e)
		sb.WriteString("; LOAD ")
		sb.WriteString(e)
		sb.WriteString("; ")
	}
}

// writeSetStmt appends `SET key='value'; ` with single quotes in value
// doubled for SQL string escaping.
func writeSetStmt(sb *strings.Builder, key, value string) {
	sb.WriteString("SET ")
	sb.WriteString(key)
	sb.WriteString("='")
	sb.WriteString(strings.ReplaceAll(value, "'", "''"))
	sb.WriteString("'; ")
}
