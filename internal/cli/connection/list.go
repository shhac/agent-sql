package connection

import (
	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

// renderConnection builds the per-row map for `connection list`. Keeps only
// the fields a human/agent needs to identify a connection; raw storage fields
// (path/url) are intentionally omitted -- display_url is the canonical view.
// Resilience to bad stored data is handled inside DisplayURL et al. (they
// return graceful zeros on parse failure); no panic recovery is needed
// here because none of the called methods can panic on JSON-roundtrippable
// data.
func renderConnection(alias string, conn config.Connection, isDefault bool) map[string]any {
	out := map[string]any{
		"alias":       alias,
		"driver":      conn.Driver,
		"default":     isDefault,
		"display_url": conn.DisplayURL(),
	}
	if host := conn.EffectiveHost(); host != "" {
		out["host"] = host
	}
	if port := conn.EffectivePort(); port != 0 {
		out["port"] = port
	}
	if conn.Database != "" {
		out["database"] = conn.Database
	}
	if conn.Credential != "" {
		out["credential"] = conn.Credential
	}
	if len(conn.Options) > 0 {
		out["options"] = conn.Options
	}
	return out
}

func registerList(parent *cobra.Command) {
	list := &cobra.Command{
		Use:   "list",
		Short: "List saved connections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conns := config.GetConnections()
			defaultAlias := config.GetDefaultAlias()

			items := make([]map[string]any, 0, len(conns))
			for alias, conn := range conns {
				items = append(items, renderConnection(alias, conn, alias == defaultAlias))
			}

			output.PrintJSON(map[string]any{"connections": items}, true)
			return nil
		},
	}
	parent.AddCommand(list)
}
