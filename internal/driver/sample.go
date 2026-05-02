package driver

import "fmt"

// SuffixLimitSelect builds `SELECT * FROM <quotedTable><where> LIMIT n`,
// the standard form for every dialect except MSSQL. Drivers whose dialect
// matches this shape delegate `BuildSampleSelect` to this helper.
//
// The CLI owns every byte of the input — no user-supplied SQL fragments
// flow in here — so plain string composition is safe.
func SuffixLimitSelect(quotedTable, whereClause string, n int) string {
	return fmt.Sprintf("SELECT * FROM %s%s LIMIT %d", quotedTable, whereClause, n)
}
