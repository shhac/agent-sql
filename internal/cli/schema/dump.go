package schema

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/driver"
	libcli "github.com/shhac/lib-agent-cli/cli"
)

// tableDump is one table's full schema in the dump output.
type tableDump struct {
	Name        string                  `json:"name"`
	Schema      string                  `json:"schema,omitempty"`
	Columns     []driver.ColumnInfo     `json:"columns"`
	Indexes     []driver.IndexInfo      `json:"indexes"`
	Constraints []driver.ConstraintInfo `json:"constraints"`
}

func registerDump(parent *cobra.Command, globals func() SchemaGlobals) {
	var tables string
	var includeSystem bool

	dump := &cobra.Command{
		Use:   "dump",
		Short: "Dump full schema (tables, columns, indexes, constraints)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				// Handle --format sql via DDLDumper interface
				if g.Format == "sql" {
					return dumpSQL(ctx, drv, tables, includeSystem)
				}

				selected, err := selectTables(ctx, drv, includeSystem, tables)
				if err != nil {
					return err
				}
				result, err := buildTableDumps(ctx, drv, selected)
				if err != nil {
					return err
				}
				printResult(map[string]any{"tables": result}, g, true, "dump")
				return nil
			})
		},
	}
	dump.Flags().StringVar(&tables, "tables", "", "Comma-separated table filter")
	dump.Flags().BoolVar(&includeSystem, "include-system", false, "Include system tables")
	// --format sql (CREATE TABLE statements) is a dump-only domain format;
	// opt it into the root --format validator for this command alone.
	libcli.AllowFormats(dump, "sql")
	parent.AddCommand(dump)
}

// selectTables lists tables and applies the comma-separated --tables filter.
// Both dump output paths (JSON envelope and --format sql DDL) select through
// here so they can't drift on which tables they include.
func selectTables(ctx context.Context, drv driver.Connection, includeSystem bool, tableFilter string) ([]driver.TableInfo, error) {
	allTables, err := drv.GetTables(ctx, includeSystem)
	if err != nil {
		return nil, err
	}
	if tableFilter == "" {
		return allTables, nil
	}
	filterSet := parseTableFilter(tableFilter)
	filtered := make([]driver.TableInfo, 0)
	for _, t := range allTables {
		if matchesFilter(t, filterSet) {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// buildTableDumps assembles the full per-table schema (columns, indexes,
// constraints) for the JSON dump envelope.
func buildTableDumps(ctx context.Context, drv driver.Connection, tables []driver.TableInfo) ([]tableDump, error) {
	result := make([]tableDump, 0, len(tables))
	for _, t := range tables {
		name := qualifiedName(t)
		columns, err := drv.DescribeTable(ctx, name)
		if err != nil {
			return nil, err
		}
		indexes, err := drv.GetIndexes(ctx, name)
		if err != nil {
			return nil, err
		}
		constraints, err := drv.GetConstraints(ctx, name)
		if err != nil {
			return nil, err
		}
		result = append(result, tableDump{
			Name:        t.Name,
			Schema:      t.Schema,
			Columns:     columns,
			Indexes:     indexes,
			Constraints: constraints,
		})
	}
	return result, nil
}

// dumpSQL outputs CREATE TABLE statements using the DDLDumper interface.
func dumpSQL(ctx context.Context, drv driver.Connection, tableFilter string, includeSystem bool) error {
	dumper, ok := drv.(driver.DDLDumper)
	if !ok {
		return fmt.Errorf("--format sql is not supported by this driver")
	}

	selected, err := selectTables(ctx, drv, includeSystem, tableFilter)
	if err != nil {
		return err
	}

	for i, t := range selected {
		name := qualifiedName(t)
		ddl, err := dumper.GetDDL(ctx, name)
		if err != nil {
			return err
		}
		if i > 0 {
			_, _ = fmt.Fprintln(os.Stdout)
		}
		_, _ = fmt.Fprintln(os.Stdout, ddl)
	}
	return nil
}

func parseTableFilter(raw string) map[string]bool {
	set := make(map[string]bool)
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = true
		}
	}
	return set
}

func matchesFilter(t driver.TableInfo, filter map[string]bool) bool {
	qualified := qualifiedName(t)
	return filter[qualified] || filter[t.Name]
}

func qualifiedName(t driver.TableInfo) string {
	if t.Schema != "" {
		return t.Schema + "." + t.Name
	}
	return t.Name
}
