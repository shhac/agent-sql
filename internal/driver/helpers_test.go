package driver

import (
	"testing"
)

func TestQuoteIdentDot(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple name", "users", `"users"`},
		{"dot-separated", "public.users", `"public"."users"`},
		{"embedded quotes", `my"table`, `"my""table"`},
		{"empty string", "", `""`},
		{"three parts", "catalog.schema.table", `"catalog"."schema"."table"`},
		{"quotes in both parts", `s"a.t"b`, `"s""a"."t""b"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteIdentDot(tt.input)
			if got != tt.want {
				t.Errorf("QuoteIdentDot(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitSchemaTable(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		defaultSchema string
		wantSchema    string
		wantTable     string
	}{
		{"with dot", "myschema.users", "public", "myschema", "users"},
		{"without dot uses default", "users", "public", "public", "users"},
		{"empty string", "", "public", "public", ""},
		{"dbo default", "orders", "dbo", "dbo", "orders"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := SplitSchemaTable(tt.input, tt.defaultSchema)
			if schema != tt.wantSchema {
				t.Errorf("schema = %q, want %q", schema, tt.wantSchema)
			}
			if table != tt.wantTable {
				t.Errorf("table = %q, want %q", table, tt.wantTable)
			}
		})
	}
}

func TestMapConstraintType(t *testing.T) {
	tests := []struct {
		input string
		want  ConstraintType
	}{
		{"PRIMARY KEY", ConstraintPrimaryKey},
		{"FOREIGN KEY", ConstraintForeignKey},
		{"UNIQUE", ConstraintUnique},
		{"CHECK", ConstraintCheck},
		{"UNKNOWN", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := MapConstraintType(tt.input)
			if got != tt.want {
				t.Errorf("MapConstraintType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want any
	}{
		{"[]byte to string", []byte("hello"), "hello"},
		{"int passthrough", 42, 42},
		{"nil passthrough", nil, nil},
		{"string passthrough", "world", "world"},
		{"float passthrough", 3.14, 3.14},
		{"bool passthrough", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeValue(tt.val)
			if got != tt.want {
				t.Errorf("NormalizeValue(%v) = %v (%T), want %v (%T)", tt.val, got, got, tt.want, tt.want)
			}
		})
	}
}
