package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/output"
)

const usageText = `config — Manage CLI settings

COMMANDS:
  config get <key>              Get a config value
  config set <key> <value>      Set a config value
  config reset                  Reset all settings to defaults
  config list-keys              List all valid keys with defaults and ranges

KEYS:
  defaults.format        (jsonl)   Default output format [jsonl, json, yaml, csv]
  defaults.limit         (20)      Default row limit for queries [1-1000]
  query.timeout          (30000)   Query timeout in ms [1000-300000]
  query.maxRows          (10000)   Maximum rows per query [1-10000]
  truncation.maxLength   (200)     String truncation threshold [50-100000]

EXAMPLES:
  agent-sql config set defaults.limit 50
  agent-sql config get query.timeout
  agent-sql config reset
`

type keyDef struct {
	key          string
	description  string
	keyType      string // "number" or "string"
	defaultValue any
	min          int
	max          int
	allowed      []string
}

var validKeys = []keyDef{
	{key: "defaults.format", description: "Default output format", keyType: "string", defaultValue: "jsonl", allowed: []string{"jsonl", "json", "yaml", "csv"}},
	{key: "defaults.limit", description: "Default row limit for queries", keyType: "number", defaultValue: 20, min: 1, max: 1000},
	{key: "query.timeout", description: "Query timeout in milliseconds", keyType: "number", defaultValue: 30000, min: 1000, max: 300000},
	{key: "query.maxRows", description: "Maximum rows per query", keyType: "number", defaultValue: 10000, min: 1, max: 10000},
	{key: "truncation.maxLength", description: "String truncation threshold", keyType: "number", defaultValue: 200, min: 50, max: 100000},
}

func findKey(key string) *keyDef {
	for i := range validKeys {
		if validKeys[i].key == key {
			return &validKeys[i]
		}
	}
	return nil
}

func validKeyNames() string {
	names := make([]string, len(validKeys))
	for i, k := range validKeys {
		names[i] = k.key
	}
	return strings.Join(names, ", ")
}

// Register adds the config command group to root.
func Register(root *cobra.Command) {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI settings",
	}

	registerGet(cfg)
	registerSet(cfg)
	registerReset(cfg)
	registerListKeys(cfg)

	cfg.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Print config command documentation (LLM-optimized)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(usageText)
		},
	})

	root.AddCommand(cfg)
}

func registerGet(parent *cobra.Command) {
	get := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			def := findKey(key)
			if def == nil {
				output.WriteError(os.Stderr, fmt.Errorf("Unknown key: %q. Valid keys: %s", key, validKeyNames()))
				return nil
			}

			value := config.GetSetting(key)
			output.PrintJSON(map[string]any{"key": key, "value": value}, true)
			return nil
		},
	}
	parent.AddCommand(get)
}

func registerSet(parent *cobra.Command) {
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, rawValue := args[0], args[1]
			def := findKey(key)
			if def == nil {
				output.WriteError(os.Stderr, fmt.Errorf("Unknown key: %q. Valid keys: %s", key, validKeyNames()))
				return nil
			}

			value, err := parseConfigValue(def, rawValue)
			if err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			if err := config.UpdateSetting(key, value); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}

			output.PrintJSON(map[string]any{"ok": true, "key": key, "value": value}, true)
			return nil
		},
	}
	parent.AddCommand(set)
}

func registerReset(parent *cobra.Command) {
	reset := &cobra.Command{
		Use:   "reset",
		Short: "Reset all settings to defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.ResetSettings(); err != nil {
				output.WriteError(os.Stderr, err)
				return nil
			}
			output.PrintJSON(map[string]any{"ok": true, "message": "Settings reset to defaults"}, true)
			return nil
		},
	}
	parent.AddCommand(reset)
}

func registerListKeys(parent *cobra.Command) {
	listKeys := &cobra.Command{
		Use:   "list-keys",
		Short: "List all valid config keys with defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := make([]map[string]any, 0, len(validKeys))
			for _, k := range validKeys {
				entry := map[string]any{
					"key":         k.key,
					"type":        k.keyType,
					"default":     k.defaultValue,
					"description": k.description,
				}
				if k.keyType == "number" {
					entry["min"] = k.min
					entry["max"] = k.max
				} else {
					entry["allowedValues"] = k.allowed
				}
				keys = append(keys, entry)
			}
			output.PrintJSON(map[string]any{"keys": keys}, true)
			return nil
		},
	}
	parent.AddCommand(listKeys)
}

func parseConfigValue(def *keyDef, raw string) (any, error) {
	if def.keyType == "string" {
		found := false
		for _, v := range def.allowed {
			if v == raw {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%q must be one of: %s. Got: %q", def.key, strings.Join(def.allowed, ", "), raw)
		}
		return raw, nil
	}

	num, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%q must be an integer. Got: %q", def.key, raw)
	}
	if num < def.min {
		return nil, fmt.Errorf("%q minimum is %d. Got: %d", def.key, def.min, num)
	}
	if num > def.max {
		return nil, fmt.Errorf("%q maximum is %d. Got: %d", def.key, def.max, num)
	}
	return num, nil
}
