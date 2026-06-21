package config

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	configpkg "github.com/shhac/agent-sql/internal/config"
)

func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	g := &shared.GlobalFlags{}
	root := &cobra.Command{
		Use:           "agent-sql",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	Register(root, func() *shared.GlobalFlags { return g })
	return root
}

// TestConfigGetUnknownKeyUnresolved confirms config get on an unknown key
// exits 0 and emits an @unresolved NDJSON record on stdout (item-level miss,
// not a command-level failure). Stderr stays silent.
func TestConfigGetUnknownKeyUnresolved(t *testing.T) {
	configpkg.SetConfigDir(t.TempDir())

	root := testRoot(t)
	root.SetArgs([]string{"config", "get", "no.such.key"})
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "@unresolved") {
		t.Errorf("stdout should contain @unresolved; got: %s", out)
	}
	if !strings.Contains(out, "no.such.key") {
		t.Errorf("stdout should name the missing key; got: %s", out)
	}
}

// TestConfigSetUnknownKeyExitsNonZero same for set.
func TestConfigSetUnknownKeyExitsNonZero(t *testing.T) {
	configpkg.SetConfigDir(t.TempDir())
	root := testRoot(t)
	root.SetArgs([]string{"config", "set", "no.such.key", "value"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

// TestConfigSetThenGetRoundTrip verifies the set/get pair works for a
// known key.
func TestConfigSetThenGetRoundTrip(t *testing.T) {
	configpkg.SetConfigDir(t.TempDir())

	root := testRoot(t)
	root.SetArgs([]string{"config", "set", "query.timeout", "60000"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("set: %v", err)
	}

	if got := configpkg.GetSetting("query.timeout"); got != float64(60000) {
		t.Errorf("query.timeout = %v, want 60000", got)
	}
}
