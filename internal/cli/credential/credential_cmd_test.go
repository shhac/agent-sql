package credential

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/output"
)

// testRoot returns a fresh cobra root with the credential commands
// registered. SilenceErrors mirrors production root.
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

// execute runs root and renders any bubbled error to stderr exactly as the
// production main (libcli.Run) does, then returns it.
func execute(root *cobra.Command) error {
	if err := root.Execute(); err != nil {
		output.WriteError(os.Stderr, err)
		return err
	}
	return nil
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

// TestCredentialRemoveNonexistentExitsNonZero confirms removing a
// missing credential produces a hard exit (the A1 contract).
func TestCredentialRemoveNonexistentExitsNonZero(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	stderr, restore := captureStderr(t)

	root := testRoot(t)
	root.SetArgs([]string{"credential", "remove", "ghost"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := execute(root)
	restore()

	if err == nil {
		t.Fatal("expected error for missing credential")
	}
	if !strings.Contains(stderr.String(), `"error"`) {
		t.Errorf("stderr should be JSON error; got: %s", stderr.String())
	}
}

// TestCredentialAddViaFlagsRoundTrip confirms credential.Store+Get
// round-trip via the cobra add command. Skipped on darwin (keychain
// prompt risk).
func TestCredentialAddViaFlagsRoundTrip(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("credential.Store invokes macOS `security`; covered on linux")
	}
	config.SetConfigDir(t.TempDir())

	root := testRoot(t)
	root.SetArgs([]string{"credential", "add", "test-cred", "--username", "u", "--password", "p"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := credential.Get("test-cred")
	if got == nil {
		t.Fatal("credential not stored")
	}
	if got.Username != "u" || got.Password != "p" {
		t.Errorf("round-trip lost values: %+v", got)
	}
}
