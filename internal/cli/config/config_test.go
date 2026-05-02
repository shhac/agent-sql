package config

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	configpkg "github.com/shhac/agent-sql/internal/config"
)

func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{
		Use:           "agent-sql",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	Register(root)
	return root
}

func captureStderr(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		_ = w.Close()
		<-done
		os.Stderr = prev
		_ = r.Close()
	}
}

// TestConfigGetUnknownKeyExitsNonZero confirms config get on an
// unknown key hard-exits (A1 contract) with a JSON error.
func TestConfigGetUnknownKeyExitsNonZero(t *testing.T) {
	configpkg.SetConfigDir(t.TempDir())
	stderr, restore := captureStderr(t)

	root := testRoot(t)
	root.SetArgs([]string{"config", "get", "no.such.key"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(stderr.String(), "unknown key") {
		t.Errorf("stderr should explain unknown key; got: %s", stderr.String())
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
