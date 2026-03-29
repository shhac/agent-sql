package driver

import "strings"

// NormalizeValue converts database driver return types to JSON-friendly types.
// Drivers with additional type conversions (e.g., PG UUIDs) should use their own version.
func NormalizeValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	default:
		return val
	}
}

// QuoteIdentDot quotes an identifier with dot-splitting for schema-qualified names.
// Uses double-quote style quoting (ANSI SQL).
func QuoteIdentDot(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

// SplitSchemaTable splits a potentially schema-qualified name into (schema, table).
// If the name contains no dot, defaultSchema is used.
func SplitSchemaTable(name, defaultSchema string) (string, string) {
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return defaultSchema, name
}

// MapConstraintType maps database constraint type strings to ConstraintType.
// Returns empty string for unrecognized types.
func MapConstraintType(s string) ConstraintType {
	switch s {
	case "PRIMARY KEY":
		return ConstraintPrimaryKey
	case "FOREIGN KEY":
		return ConstraintForeignKey
	case "UNIQUE":
		return ConstraintUnique
	case "CHECK":
		return ConstraintCheck
	default:
		return ""
	}
}
