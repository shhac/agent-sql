package shared

import (
	"fmt"

	"github.com/spf13/cobra"
)

// RegisterUsage wires the canonical `<verb> usage` subcommand. Centralised so
// the per-package fmt import and 4-line cobra block aren't duplicated.
func RegisterUsage(parent *cobra.Command, verb, text string) {
	parent.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Show detailed reference for " + verb,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(text)
		},
	})
}
