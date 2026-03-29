package driver

import (
	"errors"
	"testing"
)

func TestSliceIterator(t *testing.T) {
	rows := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}
	iter := SliceIterator([]string{"id", "name"}, rows)

	t.Run("columns", func(t *testing.T) {
		if cols := iter.Columns(); len(cols) != 2 || cols[0] != "id" {
			t.Errorf("columns = %v", cols)
		}
	})

	t.Run("iterates all rows", func(t *testing.T) {
		count := 0
		for iter.Next() {
			row, err := iter.Scan()
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 && row["name"] != "Alice" {
				t.Errorf("first row name = %v", row["name"])
			}
			count++
		}
		if count != 2 {
			t.Errorf("iterated %d rows, want 2", count)
		}
		if err := iter.Err(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("close is no-op", func(t *testing.T) {
		if err := iter.Close(); err != nil {
			t.Errorf("close error: %v", err)
		}
	})
}

func TestSliceIteratorEmpty(t *testing.T) {
	iter := SliceIterator([]string{"id"}, nil)
	if iter.Next() {
		t.Error("empty iterator should return false on Next()")
	}
	if err := iter.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCollect(t *testing.T) {
	rows := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
		{"id": 3, "name": "Charlie"},
	}
	iter := SliceIterator([]string{"id", "name"}, rows)

	result, err := Collect(iter)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 2 {
		t.Errorf("columns = %d, want 2", len(result.Columns))
	}
	if len(result.Rows) != 3 {
		t.Errorf("rows = %d, want 3", len(result.Rows))
	}
	if result.Rows[0]["name"] != "Alice" {
		t.Errorf("first row = %v", result.Rows[0])
	}
}

func TestCollectEmpty(t *testing.T) {
	iter := SliceIterator([]string{"id"}, nil)
	result, err := Collect(iter)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("rows = %d, want 0", len(result.Rows))
	}
}

func TestCollectWithError(t *testing.T) {
	scanErr := errors.New("scan failed")
	callCount := 0
	iter := NewRowIterator(
		[]string{"id"},
		func() bool { callCount++; return callCount <= 2 },
		func() (map[string]any, error) {
			if callCount == 2 {
				return nil, scanErr
			}
			return map[string]any{"id": callCount}, nil
		},
		func() error { return nil },
		func() error { return nil },
	)

	_, err := Collect(iter)
	if err == nil {
		t.Fatal("expected error from Collect")
	}
	if err != scanErr {
		t.Errorf("error = %v, want %v", err, scanErr)
	}
}

func TestRowIteratorErrAfterIteration(t *testing.T) {
	iterErr := errors.New("iteration error")
	callCount := 0
	iter := NewRowIterator(
		[]string{"id"},
		func() bool { callCount++; return callCount <= 1 },
		func() (map[string]any, error) { return map[string]any{"id": 1}, nil },
		func() error { return iterErr },
		func() error { return nil },
	)

	for iter.Next() {
		iter.Scan()
	}
	if err := iter.Err(); err != iterErr {
		t.Errorf("Err() = %v, want %v", err, iterErr)
	}
}
