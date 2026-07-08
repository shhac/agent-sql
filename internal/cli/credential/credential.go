package credential

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/output"
	libcli "github.com/shhac/lib-agent-cli/cli"
)

const usageText = `credential — Manage stored credentials for SQL database authentication

COMMANDS:
  credential add <name> [--username <user>] [--password <pass>] [--write] [--form]
    Store a named credential. Overwrites if name already exists.
    SQLite credentials may omit username/password (only writePermission matters).
    Snowflake uses a PAT (Personal Access Token) as the password.
    --write grants permission for INSERT/UPDATE/DELETE/DDL operations.
    --form pops a native OS dialog so the user can type secrets directly
           into the operating system. The LLM driving the CLI never sees
           the typed values — only a redacted JSON receipt is returned on
           stdout. Requires a graphical desktop session; fails cleanly
           with fixable_by="human" if no GUI is available (SSH, headless).

  credential remove <name>
    Remove a stored credential.

  credential list
    List all stored credentials (passwords always masked).
    Shows writePermission for each credential.

WORKFLOW:
  1. Store credential:   agent-sql credential add acme --username deploy --password secret --write
  2. Add connections:    agent-sql connection add prod --driver pg --credential acme
                         agent-sql connection add staging --driver pg --credential acme
  3. Rotate password:    agent-sql credential add acme --username deploy --password new-secret --write
     All connections referencing "acme" pick up the new password automatically.

LLM SAFETY:
  When configuring credentials on behalf of a non-technical user, never put
  pasted secrets into --password. Instead, run:
    agent-sql credential add <name> [--username <u>] [--write] --form
  The user will see a native popup on their screen and type the secret
  directly into the OS. The CLI returns only a redacted JSON receipt.

SQLITE NOTE:
  SQLite credentials typically only need --write to enable write mode.
  Username/password are optional since SQLite uses file-system permissions.
    agent-sql credential add local-write --write
    agent-sql connection add mydb --driver sqlite --path ./data.db --credential local-write

SNOWFLAKE NOTE:
  Snowflake authenticates via PAT (Personal Access Token) stored as the password.
  No --username needed — Snowflake identifies the user from the token.
    agent-sql credential add sf-cred --password <PAT>
    agent-sql connection add sf-prod --driver snowflake --account myorg-myaccount --database MYDB --credential sf-cred

KEYCHAIN (macOS):
  On macOS, credentials are stored in the system keychain automatically.
  Non-macOS falls back to plaintext config. credential list output is identical
  regardless of storage backend.

CONFIG: ~/.config/agent-sql/credentials.json (respects XDG_CONFIG_HOME)
`

// Register adds the credential command group to root.
func Register(root *cobra.Command) {
	cred := &cobra.Command{
		Use:   "credential",
		Short: "Manage stored credentials",
	}
	libcli.HandleUnknownCommand(cred, "run 'agent-sql credential usage' to see the available commands")

	registerAdd(cred)
	registerRemove(cred)
	registerList(cred)

	shared.RegisterUsage(cred, "credential", usageText)

	root.AddCommand(cred)
}

func registerAdd(parent *cobra.Command) {
	var (
		username string
		password string
		write    bool
		form     bool
	)

	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a stored credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if form {
				filledUser, filledPass, err := promptMissingViaDialog(cmd.Context(), name, username, password)
				if err != nil {
					return err
				}
				username = filledUser
				password = filledPass
			}

			storage, err := credential.Store(name, credential.Credential{
				Username:        username,
				Password:        password,
				WritePermission: write,
			})
			if err != nil {
				return err
			}

			if storage == "file" && runtime.GOOS != "darwin" {
				output.Notice("credentials stored in plaintext (macOS Keychain not available)", "")
			}

			output.PrintResult(map[string]any{
				"ok":              true,
				"credential":      name,
				"username":        username,
				"writePermission": write,
				"storage":         storage,
				"hint":            fmt.Sprintf("Use with: agent-sql connection add <alias> --driver <pg|cockroachdb|sqlite|duckdb|mysql|mariadb|snowflake|mssql> --credential %s", name),
			}, true)
			return nil
		},
	}
	add.Flags().StringVar(&username, "username", "", "Database username")
	add.Flags().StringVar(&password, "password", "", "Database password")
	add.Flags().BoolVar(&write, "write", false, "Allow write operations")
	add.Flags().BoolVar(&form, "form", false, "Prompt for missing secrets via a native OS dialog (LLM never sees the input)")
	parent.AddCommand(add)
}

func registerRemove(parent *cobra.Command) {
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a stored credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := credential.Remove(args[0]); err != nil {
				return err
			}
			output.PrintResult(map[string]any{"ok": true, "removed": args[0]}, true)
			return nil
		},
	}
	parent.AddCommand(remove)
}

func registerList(parent *cobra.Command) {
	list := &cobra.Command{
		Use:   "list",
		Short: "List stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			names := credential.List()

			items := make([]any, 0, len(names))
			for _, name := range names {
				cred := credential.Get(name)
				if cred == nil {
					continue
				}
				maskedPass := ""
				if cred.Password != "" && cred.Password != "__KEYCHAIN__" {
					maskedPass = "****"
				}
				items = append(items, map[string]any{
					"name":            name,
					"username":        cred.Username,
					"password":        maskedPass,
					"writePermission": cred.WritePermission,
				})
			}

			output.PrintList(items, nil, true)
			return nil
		},
	}
	parent.AddCommand(list)
}
