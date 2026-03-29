package driver

import "database/sql"

// SQLRowsIterator wraps *sql.Rows into a RowIterator.
// Used by drivers backed by database/sql (SQLite, MySQL, MSSQL).
func SQLRowsIterator(rows *sql.Rows, normalize func(any) any) (*RowIterator, error) {
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}

	nCols := len(columns)
	values := make([]any, nCols)
	ptrs := make([]any, nCols)
	for i := range values {
		ptrs[i] = &values[i]
	}

	return NewRowIterator(
		columns,
		func() bool { return rows.Next() },
		func() (map[string]any, error) {
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			row := make(map[string]any, nCols)
			for i, col := range columns {
				if normalize != nil {
					row[col] = normalize(values[i])
				} else {
					row[col] = values[i]
				}
			}
			// Reset values for next scan
			for i := range values {
				values[i] = nil
			}
			return row, nil
		},
		func() error { return rows.Err() },
		func() error { return rows.Close() },
	), nil
}
