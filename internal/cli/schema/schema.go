package schema

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/output"
	libcli "github.com/shhac/lib-agent-cli/cli"
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
  agent-sql schema dump --format sql               CREATE TABLE statements (driver-dependent)
  agent-sql schema dump --include-system           Include system tables

OPTIONS
  -c, --connection <alias>    Connection to use (default: configured default)
  --detailed                  Include constraints, indexes, and comments (describe only)
  --include-system            Include system/internal tables (tables, dump)
  --type <type>               Filter constraint type: pk, fk, unique, check
  --tables <list>             Comma-separated table names (dump only)
  --compact                   Typed NDJSON output for schema commands

OUTPUT FORMAT
  Lists (tables, indexes, constraints) return NDJSON records by default —
  one JSON object per line; --format json/yaml wraps them in {"data": [...]}.
  Single resources (describe, search, dump) return one JSON line by default,
  pretty JSON with --format json.
  --compact: {"type":"tables","values":{...}}
  --format sql: CREATE TABLE statements (schema dump only)
  Errors: { "error": "...", "fixable_by": "agent"|"human" } to stderr.

WORKFLOW
  1. schema tables               List what's available
  2. schema describe <table>     Inspect columns and types
  3. schema indexes <table>      Check index coverage
  4. schema constraints <table>  Understand relationships
  5. schema search <pattern>     Find tables/columns by name
`

// SchemaGlobals holds the global flags relevant to schema commands.
type SchemaGlobals struct {
	Connection string
	TimeoutMS  int
	Format     string
	Compact    bool
}

// printResult outputs a single schema resource (describe, search, dump)
// honoring the resolved --format. When compact is true, the data is wrapped in
// a typed NDJSON message using the provided schemaType (e.g. "describe").
func printResult(data any, g SchemaGlobals, prune bool, schemaType string) {
	if g.Compact {
		printCompact(data, schemaType)
		return
	}
	output.PrintResult(data, prune)
}

// printList outputs list-shaped schema data (tables, indexes, constraints) in
// the family list contract: NDJSON records by default, a {"data": [...]}
// envelope for json/yaml. Compact keeps its wrapped one-line shape.
func printList[T any](items []T, g SchemaGlobals, schemaType string) {
	if g.Compact {
		printCompact(map[string]any{schemaType: items}, schemaType)
		return
	}
	widened := make([]any, len(items))
	for i, it := range items {
		widened[i] = it
	}
	output.PrintList(widened, nil, true)
}

// printCompact writes a single typed NDJSON line for schema output, through
// the shared funnel so it colorizes on a terminal.
func printCompact(data any, schemaType string) {
	_ = output.WriteTypedLine(os.Stdout, schemaType, data)
}

// Register adds the schema command group to root.
func Register(root *cobra.Command, globals func() SchemaGlobals) {
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Explore database schema",
	}
	libcli.HandleUnknownCommand(schema, "run 'agent-sql schema usage' to see the available commands")

	registerTables(schema, globals)
	registerDescribe(schema, globals)
	registerIndexes(schema, globals)
	registerConstraints(schema, globals)
	registerSearch(schema, globals)
	registerDump(schema, globals)

	shared.RegisterUsage(schema, "schema", usageText)

	root.AddCommand(schema)
}

func registerTables(parent *cobra.Command, globals func() SchemaGlobals) {
	var includeSystem bool

	tables := &cobra.Command{
		Use:   "tables",
		Short: "List all tables",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				result, err := drv.GetTables(ctx, includeSystem)
				if err != nil {
					return err
				}
				printList(result, g, "tables")
				return nil
			})
		},
	}
	tables.Flags().BoolVar(&includeSystem, "include-system", false, "Include system tables")
	parent.AddCommand(tables)
}

func registerDescribe(parent *cobra.Command, globals func() SchemaGlobals) {
	var detailed bool

	describe := &cobra.Command{
		Use:   "describe <table>",
		Short: "Describe a table's columns, types, and constraints",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
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

				printResult(result, g, true, "describe")
				return nil
			})
		},
	}
	describe.Flags().BoolVar(&detailed, "detailed", false, "Include constraints, indexes, and comments")
	parent.AddCommand(describe)
}

func registerIndexes(parent *cobra.Command, globals func() SchemaGlobals) {
	indexes := &cobra.Command{
		Use:   "indexes [table]",
		Short: "Show indexes for a table or all tables",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				table := ""
				if len(args) > 0 {
					table = args[0]
				}
				result, err := drv.GetIndexes(ctx, table)
				if err != nil {
					return err
				}
				printList(result, g, "indexes")
				return nil
			})
		},
	}
	parent.AddCommand(indexes)
}

func registerConstraints(parent *cobra.Command, globals func() SchemaGlobals) {
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
					return fmt.Errorf(
						"invalid constraint type: %q; valid types: pk, fk, unique, check", constraintType,
					)
				}
			}

			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
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

				printList(result, g, "constraints")
				return nil
			})
		},
	}
	constraints.Flags().StringVar(&constraintType, "type", "", "Filter by type: pk, fk, unique, check")
	parent.AddCommand(constraints)
}

func registerSearch(parent *cobra.Command, globals func() SchemaGlobals) {
	search := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search table and column names by pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globals()
			return shared.WithConnection(g.Connection, g.TimeoutMS, func(ctx context.Context, drv driver.Connection) error {
				result, err := drv.SearchSchema(ctx, args[0])
				if err != nil {
					return err
				}
				printResult(result, g, true, "search")
				return nil
			})
		},
	}
	parent.AddCommand(search)
}
