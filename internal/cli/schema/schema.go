package schema

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
)

const usageText = `SCHEMA COMMANDS
===============

List tables:
  agent-sql schema tables                          All user tables
  agent-sql schema tables --include-system         Include system/internal tables

Describe a table:
  agent-sql schema describe <table>                Columns, types, nullability
  agent-sql schema describe <table> --detailed     Include constraints, indexes, and comments
  agent-sql schema describe public.users           PG namespace (schema.table)

Indexes:
  agent-sql schema indexes                         All indexes across all tables
  agent-sql schema indexes <table>                 Indexes for a specific table

Constraints:
  agent-sql schema constraints                     All constraints across all tables
  agent-sql schema constraints <table>             Constraints for a specific table
  agent-sql schema constraints <table> --type pk   Filter by type (pk, fk, unique, check)

Search:
  agent-sql schema search <pattern>                Search table and column names by pattern

Dump full schema:
  agent-sql schema dump                            DDL-style dump of all tables
  agent-sql schema dump --tables users,orders      Dump specific tables only
  agent-sql schema dump --include-system           Include system tables

OPTIONS
  -c, --connection <alias>    Connection to use (default: configured default)
  --detailed                  Include constraints, indexes, and comments (describe only)
  --include-system            Include system/internal tables (tables, dump)
  --type <type>               Filter constraint type: pk, fk, unique, check
  --tables <list>             Comma-separated table names (dump only)

OUTPUT FORMAT
  All commands return JSON to stdout.
  Errors: { "error": "...", "fixable_by": "agent"|"human" } to stderr.

WORKFLOW
  1. schema tables               List what's available
  2. schema describe <table>     Inspect columns and types
  3. schema indexes <table>      Check index coverage
  4. schema constraints <table>  Understand relationships
  5. schema search <pattern>     Find tables/columns by name
`

// printResult outputs data in the appropriate format based on the format flag.
func printResult(data any, formatFlag string, prune bool) {
	format := output.ResolveFormat(formatFlag)
	switch format {
	case output.FormatYAML:
		output.PrintYAML(os.Stdout, data)
	default:
		output.PrintJSON(data, prune)
	}
}

// Register adds the schema command group to root.
func Register(root *cobra.Command, globals func() (string, int, string)) {
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Explore database schema",
	}

	registerTables(schema, globals)
	registerDescribe(schema, globals)
	registerIndexes(schema, globals)
	registerConstraints(schema, globals)
	registerSearch(schema, globals)
	registerDump(schema, globals)

	schema.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Print schema command reference (LLM-optimized)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(usageText)
		},
	})

	root.AddCommand(schema)
}

func registerTables(parent *cobra.Command, globals func() (string, int, string)) {
	var includeSystem bool

	tables := &cobra.Command{
		Use:   "tables",
		Short: "List all tables",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				result, err := drv.GetTables(ctx, includeSystem)
				if err != nil {
					return err
				}
				printResult(map[string]any{"tables": result}, formatFlag, true)
				return nil
			})
		},
	}
	tables.Flags().BoolVar(&includeSystem, "include-system", false, "Include system tables")
	parent.AddCommand(tables)
}

func registerDescribe(parent *cobra.Command, globals func() (string, int, string)) {
	var detailed bool

	describe := &cobra.Command{
		Use:   "describe <table>",
		Short: "Describe a table's columns, types, and constraints",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				table := args[0]
				columns, err := drv.DescribeTable(ctx, table)
				if err != nil {
					return err
				}

				result := map[string]any{"table": table, "columns": columns}

				if detailed {
					constraints, cErr := drv.GetConstraints(ctx, table)
					if cErr != nil {
						return cErr
					}
					indexes, iErr := drv.GetIndexes(ctx, table)
					if iErr != nil {
						return iErr
					}
					result["constraints"] = constraints
					result["indexes"] = indexes
				}

				printResult(result, formatFlag, true)
				return nil
			})
		},
	}
	describe.Flags().BoolVar(&detailed, "detailed", false, "Include constraints, indexes, and comments")
	parent.AddCommand(describe)
}

func registerIndexes(parent *cobra.Command, globals func() (string, int, string)) {
	indexes := &cobra.Command{
		Use:   "indexes [table]",
		Short: "Show indexes for a table or all tables",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				table := ""
				if len(args) > 0 {
					table = args[0]
				}
				result, err := drv.GetIndexes(ctx, table)
				if err != nil {
					return err
				}
				printResult(map[string]any{"indexes": result}, formatFlag, true)
				return nil
			})
		},
	}
	parent.AddCommand(indexes)
}

func registerConstraints(parent *cobra.Command, globals func() (string, int, string)) {
	var constraintType string

	typeMap := map[string]driver.ConstraintType{
		"pk":     driver.ConstraintPrimaryKey,
		"fk":     driver.ConstraintForeignKey,
		"unique": driver.ConstraintUnique,
		"check":  driver.ConstraintCheck,
	}

	constraints := &cobra.Command{
		Use:   "constraints [table]",
		Short: "Show constraints for a table or all tables",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if constraintType != "" {
				if _, ok := typeMap[constraintType]; !ok {
					output.WriteError(os.Stderr, fmt.Errorf(
						"Invalid constraint type: %q. Valid types: pk, fk, unique, check", constraintType,
					))
					return nil
				}
			}

			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				table := ""
				if len(args) > 0 {
					table = args[0]
				}

				result, err := drv.GetConstraints(ctx, table)
				if err != nil {
					return err
				}

				if constraintType != "" {
					filtered := make([]driver.ConstraintInfo, 0, len(result))
					target := typeMap[constraintType]
					for _, c := range result {
						if c.Type == target {
							filtered = append(filtered, c)
						}
					}
					result = filtered
				}

				printResult(map[string]any{"constraints": result}, formatFlag, true)
				return nil
			})
		},
	}
	constraints.Flags().StringVar(&constraintType, "type", "", "Filter by type: pk, fk, unique, check")
	parent.AddCommand(constraints)
}

func registerSearch(parent *cobra.Command, globals func() (string, int, string)) {
	search := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search table and column names by pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				result, err := drv.SearchSchema(ctx, args[0])
				if err != nil {
					return err
				}
				printResult(result, formatFlag, true)
				return nil
			})
		},
	}
	parent.AddCommand(search)
}

func registerDump(parent *cobra.Command, globals func() (string, int, string)) {
	var tables string
	var includeSystem bool

	dump := &cobra.Command{
		Use:   "dump",
		Short: "Dump full schema (tables, columns, indexes, constraints)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, timeout, formatFlag := globals()
			return shared.WithConnection(conn, timeout, func(ctx context.Context, drv driver.Connection) error {
				allTables, err := drv.GetTables(ctx, includeSystem)
				if err != nil {
					return err
				}

				filtered := allTables
				if tables != "" {
					filterSet := parseTableFilter(tables)
					filtered = make([]driver.TableInfo, 0)
					for _, t := range allTables {
						if matchesFilter(t, filterSet) {
							filtered = append(filtered, t)
						}
					}
				}

				type tableDump struct {
					Name        string                  `json:"name"`
					Schema      string                  `json:"schema,omitempty"`
					Columns     []driver.ColumnInfo      `json:"columns"`
					Indexes     []driver.IndexInfo       `json:"indexes"`
					Constraints []driver.ConstraintInfo  `json:"constraints"`
				}

				result := make([]tableDump, 0, len(filtered))
				for _, t := range filtered {
					name := qualifiedName(t)
					columns, cErr := drv.DescribeTable(ctx, name)
					if cErr != nil {
						return cErr
					}
					indexes, iErr := drv.GetIndexes(ctx, name)
					if iErr != nil {
						return iErr
					}
					constraints, kErr := drv.GetConstraints(ctx, name)
					if kErr != nil {
						return kErr
					}
					result = append(result, tableDump{
						Name:        t.Name,
						Schema:      t.Schema,
						Columns:     columns,
						Indexes:     indexes,
						Constraints: constraints,
					})
				}

				printResult(map[string]any{"tables": result}, formatFlag, true)
				return nil
			})
		},
	}
	dump.Flags().StringVar(&tables, "tables", "", "Comma-separated table filter")
	dump.Flags().BoolVar(&includeSystem, "include-system", false, "Include system tables")
	parent.AddCommand(dump)
}

// helpers

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
