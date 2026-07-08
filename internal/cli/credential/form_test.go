package credential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-sql/internal/cli/shared"
	"github.com/shhac/agent-sql/internal/config"
	agenterrors "github.com/shhac/agent-sql/internal/errors"
	"github.com/shhac/lib-agent-cli/dialog"
	"github.com/shhac/lib-agent-cli/dialog/dialogtest"
)

func TestPromptMissingViaDialogReturnsEarlyWhenAllFlagsSupplied(t *testing.T) {
	rec := &dialogtest.Recorder{
		PromptResults: []dialog.Result{{ID: "username", Value: "should not be used"}},
	}
	defer dialog.SetDefault(rec)()

	user, pass, err := promptMissingViaDialog(context.Background(), "acme", "deploy", "secret")
	if err != nil {
		t.Fatalf("promptMissingViaDialog() error = %v", err)
	}
	if user != "deploy" || pass != "secret" {
		t.Fatalf("returned user/pass = %q/%q, want deploy/secret", user, pass)
	}
	if len(rec.Calls) != 0 {
		t.Errorf("Prompt should not have been called, got %d calls", len(rec.Calls))
	}
}

func TestPromptMissingViaDialogPromptsOnlyMissingPassword(t *testing.T) {
	rec := &dialogtest.Recorder{
		PromptResults: []dialog.Result{{ID: "password", Value: "from-dialog"}},
	}
	defer dialog.SetDefault(rec)()

	user, pass, err := promptMissingViaDialog(context.Background(), "acme", "deploy", "")
	if err != nil {
		t.Fatalf("promptMissingViaDialog() error = %v", err)
	}
	if user != "deploy" {
		t.Errorf("username = %q, want unchanged 'deploy'", user)
	}
	if pass != "from-dialog" {
		t.Errorf("password = %q, want 'from-dialog'", pass)
	}
	if len(rec.Calls) != 1 {
		t.Fatalf("expected 1 prompt call, got %d", len(rec.Calls))
	}
	spec := rec.Calls[0]
	if len(spec.Items) != 1 {
		t.Fatalf("expected 1 field in spec, got %d", len(spec.Items))
	}
	if spec.Items[0].ID != "password" || spec.Items[0].InputType != dialog.Password {
		t.Errorf("spec field = %+v, want password/Password", spec.Items[0])
	}
	if !strings.Contains(spec.Title, "acme") {
		t.Errorf("spec title = %q, want it to contain credential name", spec.Title)
	}
}

func TestPromptMissingViaDialogPromptsBothFieldsWhenBothMissing(t *testing.T) {
	rec := &dialogtest.Recorder{
		PromptResults: []dialog.Result{
			{ID: "username", Value: "alice"},
			{ID: "password", Value: "p4ss"},
		},
	}
	defer dialog.SetDefault(rec)()

	user, pass, err := promptMissingViaDialog(context.Background(), "acme", "", "")
	if err != nil {
		t.Fatalf("promptMissingViaDialog() error = %v", err)
	}
	if user != "alice" || pass != "p4ss" {
		t.Fatalf("user/pass = %q/%q, want alice/p4ss", user, pass)
	}
	spec := rec.Calls[0]
	if len(spec.Items) != 2 {
		t.Fatalf("expected 2 fields, got %d: %+v", len(spec.Items), spec.Items)
	}
	if spec.Items[0].ID != "username" || spec.Items[0].InputType != dialog.Text {
		t.Errorf("first field = %+v, want username/Text", spec.Items[0])
	}
	if spec.Items[1].ID != "password" || spec.Items[1].InputType != dialog.Password {
		t.Errorf("second field = %+v, want password/Password", spec.Items[1])
	}
}

func TestPromptMissingViaDialogReturnsHumanErrorWhenNoGUI(t *testing.T) {
	rec := &dialogtest.Recorder{
		AvailableErr: fmt.Errorf("%w: SSH session detected", dialog.ErrNoGUI),
	}
	defer dialog.SetDefault(rec)()

	_, _, err := promptMissingViaDialog(context.Background(), "acme", "deploy", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var qerr *agenterrors.QueryError
	if !errors.As(err, &qerr) {
		t.Fatalf("expected *QueryError, got %T", err)
	}
	if qerr.FixableBy != agenterrors.FixableByHuman {
		t.Errorf("FixableBy = %q, want human", qerr.FixableBy)
	}
	if !strings.Contains(qerr.Hint, "graphical desktop") {
		t.Errorf("hint = %q, want it to mention graphical desktop fallback", qerr.Hint)
	}
	if !strings.Contains(qerr.Hint, "--password") {
		t.Errorf("hint = %q, want it to suggest the non-interactive fallback", qerr.Hint)
	}
	// Sentinel chain must be preserved so callers can errors.Is downstream.
	if !errors.Is(err, dialog.ErrNoGUI) {
		t.Errorf("errors.Is(err, ErrNoGUI) = false, want true (sentinel chain broken)")
	}
}

func TestPromptMissingViaDialogReturnsRetryErrorOnCancel(t *testing.T) {
	rec := &dialogtest.Recorder{
		PromptErr: fmt.Errorf("%w (Database password)", dialog.ErrCancelled),
	}
	defer dialog.SetDefault(rec)()

	_, _, err := promptMissingViaDialog(context.Background(), "acme", "deploy", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var qerr *agenterrors.QueryError
	if !errors.As(err, &qerr) {
		t.Fatalf("expected *QueryError, got %T", err)
	}
	if qerr.FixableBy != agenterrors.FixableByRetry {
		t.Errorf("FixableBy = %q, want retry", qerr.FixableBy)
	}
	if !strings.Contains(qerr.Hint, "cancelled") && !strings.Contains(qerr.Hint, "Re-run") {
		t.Errorf("hint = %q, should mention cancellation and re-run", qerr.Hint)
	}
	// Sentinel chain must be preserved so callers can errors.Is downstream.
	if !errors.Is(err, dialog.ErrCancelled) {
		t.Errorf("errors.Is(err, ErrCancelled) = false, want true (sentinel chain broken)")
	}
}

// TestCredentialAddFormDoesNotLeakSecretToStdout is the load-bearing test
// for this package's headline claim: the LLM driving the CLI must never
// see the secret the user types into the dialog.
//
// We run the full cobra tree end-to-end (so the actual on-success
// PrintJSON path is exercised), feed a distinctive canary string through
// the Recorder, redirect os.Stdout to a pipe, and assert the canary does
// not appear in the captured bytes.
//
// Skipped on darwin because credential.Store shells out to the `security`
// CLI, which can pop a system GUI prompt when no unlocked keychain is
// available (e.g. fresh tempdir HOME). The leak path under test
// (PrintJSON receipt construction) is platform-independent, so coverage
// from linux/CI is sufficient to defend the claim.
func TestCredentialAddFormDoesNotLeakSecretToStdout(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("credential.Store invokes macOS `security` which can prompt; coverage runs on linux/CI")
	}
	config.SetConfigDir(t.TempDir())

	const canary = "TOPSECRET-CANARY-7A3F"
	rec := &dialogtest.Recorder{
		PromptResults: []dialog.Result{
			{ID: "password", Value: canary},
		},
	}
	defer dialog.SetDefault(rec)()

	stdout, restore := captureStdout(t)

	root := &cobra.Command{Use: "agent-sql"}
	Register(root, func() *shared.GlobalFlags { return &shared.GlobalFlags{} })
	root.SetArgs([]string{"credential", "add", "leak-test", "--username", "deploy", "--form"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	restore()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	captured := stdout.String()

	if strings.Contains(captured, canary) {
		t.Fatalf("canary %q leaked to stdout: %s", canary, captured)
	}
	// Sanity: the receipt should still be there (just without the secret).
	if !strings.Contains(captured, "leak-test") {
		t.Errorf("expected receipt to include credential name, got: %s", captured)
	}
	if !strings.Contains(captured, "deploy") {
		t.Errorf("expected receipt to include username, got: %s", captured)
	}
}

// TestBuildCredentialSpec verifies that only blank fields are added to
// the spec, and that result slots line up with the items.
func TestBuildCredentialSpec(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
		wantIDs  []string
	}{
		{"both supplied — empty spec", "deploy", "secret", nil},
		{"password only missing — one item", "deploy", "", []string{"password"}},
		{"username only missing — one item", "", "secret", []string{"username"}},
		{"both missing — two items", "", "", []string{"username", "password"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, pass := tc.username, tc.password
			spec, slots := buildCredentialSpec("acme", &user, &pass)

			if len(spec.Items) != len(tc.wantIDs) {
				t.Fatalf("len(spec.Items) = %d, want %d", len(spec.Items), len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if spec.Items[i].ID != want {
					t.Errorf("spec.Items[%d].ID = %q, want %q", i, spec.Items[i].ID, want)
				}
				if slots[i].field.ID != want {
					t.Errorf("slots[%d].field.ID = %q, want %q", i, slots[i].field.ID, want)
				}
			}
			if !strings.Contains(spec.Title, "acme") {
				t.Errorf("spec.Title = %q, want it to contain credential name", spec.Title)
			}
		})
	}
}

// TestApplyResultsMatchesByID confirms that result-folding is robust to
// reordering: if a future change reorders Spec.Items, the values still
// land in the correct slot via ID lookup.
func TestApplyResultsMatchesByID(t *testing.T) {
	user, pass := "", ""
	slots := []fieldSlot{
		{field: dialog.Field{ID: "username"}, dest: &user},
		{field: dialog.Field{ID: "password"}, dest: &pass},
	}
	// Results in REVERSE order — applyResults must still place them correctly.
	applyResults([]dialog.Result{
		{ID: "password", Value: "pw"},
		{ID: "username", Value: "alice"},
	}, slots)
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
	}
	if pass != "pw" {
		t.Errorf("pass = %q, want pw", pass)
	}
}

func TestApplyResultsIgnoresUnknownIDs(t *testing.T) {
	user := ""
	slots := []fieldSlot{
		{field: dialog.Field{ID: "username"}, dest: &user},
	}
	applyResults([]dialog.Result{
		{ID: "username", Value: "alice"},
		{ID: "extraneous", Value: "should-be-ignored"},
	}, slots)
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
	}
}

func TestCategoryToFixableBy(t *testing.T) {
	cases := map[dialog.Category]agenterrors.FixableBy{
		dialog.CategoryHuman:              agenterrors.FixableByHuman,
		dialog.CategoryRetry:              agenterrors.FixableByRetry,
		dialog.CategoryAgent:              agenterrors.FixableByAgent,
		dialog.Category("unknown-future"): agenterrors.FixableByAgent,
	}
	for in, want := range cases {
		t.Run(string(in), func(t *testing.T) {
			if got := categoryToFixableBy(in); got != want {
				t.Errorf("categoryToFixableBy(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

// captureStdout redirects os.Stdout to a pipe and returns a buffer that
// will receive everything written to stdout. The returned restore
// function must be called to put stdout back.
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
