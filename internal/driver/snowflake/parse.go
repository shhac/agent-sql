package snowflake

import (
	"encoding/json"
	"strconv"
	"strings"
)

func extractColumns(rowType []columnType) []string {
	cols := make([]string, len(rowType))
	for i, col := range rowType {
		cols[i] = col.Name
	}
	return cols
}

func parseRows(data [][]*string, rowType []columnType) []map[string]any {
	rows := make([]map[string]any, 0, len(data))
	for _, rawRow := range data {
		row := make(map[string]any, len(rowType))
		for i, col := range rowType {
			if i < len(rawRow) {
				row[col.Name] = parseValue(rawRow[i], col)
			} else {
				row[col.Name] = nil
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// ParseValue converts a raw Snowflake string value to the appropriate Go type.
// Exported for testing.
func ParseValue(raw *string, col columnType) any {
	return parseValue(raw, col)
}

func parseValue(raw *string, col columnType) any {
	if raw == nil {
		return nil
	}
	v := *raw

	switch strings.ToLower(col.Type) {
	case "fixed":
		scale := 0
		if col.Scale != nil {
			scale = *col.Scale
		}
		if scale == 0 {
			n, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				return n
			}
			return v // keep as string for very large numbers
		}
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
		return v

	case "real", "float", "double":
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
		return v

	case "boolean":
		return strings.EqualFold(v, "true") || v == "1"

	case "text", "varchar", "char", "string":
		return v

	case "variant", "object", "array", "map":
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			return parsed
		}
		return v

	case "date", "time", "timestamp_ltz", "timestamp_ntz", "timestamp_tz", "binary":
		return v

	default:
		return v
	}
}
