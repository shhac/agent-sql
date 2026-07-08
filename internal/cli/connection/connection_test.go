package connection

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
	agentout "github.com/shhac/lib-agent-output"
)

// testRoot returns a fresh cobra root with the connection commands
// registered. SilenceErrors is on so a failing RunE returns the error
// without cobra printing it -- matches the production root.
func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{
		Use:           "agent-sql",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	Register(root, func() (string, int) { return "", 0 })
	return root
}

// execute runs root and renders any bubbled error to stderr exactly as the
// production main (libcli.Run) does, then returns it.
func execute(root *cobra.Command) error {
	if err := root.Execute(); err != nil {
		agentout.WriteError(os.Stderr, err)
		return err
	}
	return nil
}

// captureStdout swaps os.Stdout for a pipe; output.PrintResult writes
// directly to os.Stdout so we can't use cmd.SetOut.
func captureStdout(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		_ = w.Close()
		<-done
		os.Stdout = prev
		_ = r.Close()
	}
}

// captureStderr is identical for stderr; agentout.WriteError writes there.
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

// TestConnectionAddRejectsEmbeddedCredentialsWithoutCredentialFlag is the
// security-relevant smoke: when a user pastes postgres://user:pass@host/db
// without --credential, the command must exit non-zero and not persist
// anything to the config file. This is the leak guard.
func TestConnectionAddRejectsEmbeddedCredentialsWithoutCredentialFlag(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	stderr, restore := captureStderr(t)

	root := testRoot(t)
	root.SetArgs([]string{"connection", "add", "leak", "postgres://u:secret@h/d"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := execute(root)
	restore() // flush pipe before reading the buffer
	if err == nil {
		t.Fatal("expected non-nil error from Execute (rejection)")
	}
	if got := config.GetConnection("leak"); got != nil {
		t.Errorf("connection must not be stored on rejection; got %+v", got)
	}
	stderrText := stderr.String()
	if !strings.Contains(stderrText, "embedded credentials") {
		t.Errorf("stderr should explain rejection; got: %s", stderrText)
	}
	if !strings.Contains(stderrText, `"fixable_by":"human"`) {
		t.Errorf("error must be FixableByHuman so an LLM agent escalates; got: %s", stderrText)
	}
	if strings.Contains(stderrText, "secret") {
		t.Errorf("password must not appear in error output; got: %s", stderrText)
	}
}

// TestConnectionAddStripsEmbeddedCredentialsWhenCredentialFlagProvided
// verifies the warn-and-strip path: with --credential, embedded user:pass@
// is removed from the stored URL and the connection is persisted with the
// cleaned URL plus the cred reference.
func TestConnectionAddStripsEmbeddedCredentialsWhenCredentialFlagProvided(t *testing.T) {
	if runtime.GOOS == "darwin" {
		// credential.Store invokes the macOS `security` CLI which can
		// prompt for keychain access in a fresh tempdir HOME. The CI
		// matrix covers linux which exercises the file fallback path.
		t.Skip("seeds a credential via keychain; covered on linux")
	}
	config.SetConfigDir(t.TempDir())

	if _, err := credential.Store("pg-cred", credential.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	stdout, restoreOut := captureStdout(t)
	stderr, restoreErr := captureStderr(t)

	root := testRoot(t)
	root.SetArgs([]string{
		"connection", "add", "ok",
		"postgres://leakuser:leaksecret@h.example.com/d",
		"--credential", "pg-cred",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	restoreErr()
	restoreOut()
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	got := config.GetConnection("ok")
	if got == nil {
		t.Fatal("connection should be stored after warn-and-strip")
	}
	if strings.Contains(got.URL, "leakuser") || strings.Contains(got.URL, "leaksecret") {
		t.Errorf("stored URL still contains credentials: %q", got.URL)
	}
	if got.Credential != "pg-cred" {
		t.Errorf("Credential = %q, want pg-cred", got.Credential)
	}
	if !strings.Contains(stderr.String(), "stripped embedded credentials") {
		t.Errorf("expected warning on stderr; got: %s", stderr.String())
	}
}

// TestConnectionUpdateUrlPreservesExistingCredential covers the
// effectiveCred fallback at registerUpdate's --url branch: when the user
// updates a URL but doesn't pass --credential, the existing connection's
// credential must satisfy the embedded-creds check.
func TestConnectionUpdateUrlPreservesExistingCredential(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("seeds a credential via keychain; covered on linux")
	}
	config.SetConfigDir(t.TempDir())
	if _, err := credential.Store("pg-cred", credential.Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := config.StoreConnection("base", config.Connection{
		Driver: "pg", Host: "old.host", Database: "d", Credential: "pg-cred",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	stdout, restoreOut := captureStdout(t)
	stderr, restoreErr := captureStderr(t)

	root := testRoot(t)
	root.SetArgs([]string{
		"connection", "update", "base",
		"--url", "postgres://leakuser:leaksecret@new.host/d",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	restoreErr()
	restoreOut()
	if err != nil {
		t.Fatalf("Execute: %v\nstderr: %s\nstdout: %s", err, stderr.String(), stdout.String())
	}

	got := config.GetConnection("base")
	if got == nil {
		t.Fatal("connection lost during update")
	}
	if strings.Contains(got.URL, "leakuser") {
		t.Errorf("stored URL still contains credentials: %q", got.URL)
	}
	if got.Credential != "pg-cred" {
		t.Errorf("existing credential should be preserved; got %q", got.Credential)
	}
}

// TestConnectionUpdateRejectsEmbeddedCredsWhenNoCredentialAvailable covers
// the inverse of the previous test: if an existing connection has no
// credential AND the user updates --url with embedded creds AND doesn't
// supply --credential, the update must be rejected.
func TestConnectionUpdateRejectsEmbeddedCredsWhenNoCredentialAvailable(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	if err := config.StoreConnection("nocred", config.Connection{
		Driver: "pg", Host: "old.host", Database: "d",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	root := testRoot(t)
	root.SetArgs([]string{
		"connection", "update", "nocred",
		"--url", "postgres://u:secret@h/d",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	if err := root.Execute(); err == nil {
		t.Fatal("expected rejection when no credential is set anywhere")
	}
	got := config.GetConnection("nocred")
	if got == nil || got.Host != "old.host" {
		t.Errorf("connection mutated despite rejection: %+v", got)
	}
}

// TestConnectionUpdateClearOptionsThenOption pins the documented
// "clear before merge" semantic: --clear-options + --option key=value
// must yield exactly the new option, not the merge with prior state.
func TestConnectionUpdateClearOptionsThenOption(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	if err := config.StoreConnection("opt", config.Connection{
		Driver:  "pg",
		Host:    "h",
		Options: map[string]string{"old_key": "old_value", "another": "x"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root := testRoot(t)
	root.SetArgs([]string{
		"connection", "update", "opt",
		"--clear-options",
		"--option", "fresh=value",
	})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := config.GetConnection("opt")
	if got == nil {
		t.Fatal("connection lost")
	}
	if len(got.Options) != 1 || got.Options["fresh"] != "value" {
		t.Errorf("options = %v, want {fresh:value} only (cleared then set)", got.Options)
	}
}

// TestConnectionUpdateClearOptionsAlone wipes options entirely.
func TestConnectionUpdateClearOptionsAlone(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	if err := config.StoreConnection("opt", config.Connection{
		Driver:  "pg",
		Host:    "h",
		Options: map[string]string{"k1": "v1", "k2": "v2"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root := testRoot(t)
	root.SetArgs([]string{"connection", "update", "opt", "--clear-options"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := config.GetConnection("opt")
	if len(got.Options) != 0 {
		t.Errorf("options should be empty after --clear-options; got %v", got.Options)
	}
}

// TestConnectionUpdateOptionMergesWithExisting verifies that --option
// without --clear-options merges into the existing options map.
func TestConnectionUpdateOptionMergesWithExisting(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	if err := config.StoreConnection("opt", config.Connection{
		Driver:  "pg",
		Host:    "h",
		Options: map[string]string{"keep": "yes"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	root := testRoot(t)
	root.SetArgs([]string{"connection", "update", "opt", "--option", "added=now"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := config.GetConnection("opt")
	if got.Options["keep"] != "yes" || got.Options["added"] != "now" {
		t.Errorf("options merge failed: %v", got.Options)
	}
}
