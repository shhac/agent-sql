package dialog_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/dialog"
	"github.com/shhac/agent-sql/internal/dialog/dialogtest"
)

func TestSetDefaultRestoresPrevious(t *testing.T) {
	original := dialog.Default
	rec := &dialogtest.Recorder{}
	restore := dialog.SetDefault(rec)
	if dialog.Default != rec {
		t.Fatalf("Default = %T, want recorder", dialog.Default)
	}
	restore()
	if dialog.Default != original {
		t.Fatalf("Default after restore = %T, want original %T", dialog.Default, original)
	}
}

func TestSetDefaultNestedSwap(t *testing.T) {
	original := dialog.Default
	first := &dialogtest.Recorder{}
	second := &dialogtest.Recorder{}

	r1 := dialog.SetDefault(first)
	r2 := dialog.SetDefault(second)

	if dialog.Default != second {
		t.Fatalf("Default = %T, want second", dialog.Default)
	}
	r2()
	if dialog.Default != first {
		t.Fatalf("Default after r2 restore = %T, want first", dialog.Default)
	}
	r1()
	if dialog.Default != original {
		t.Fatalf("Default after both restores = %T, want original %T", dialog.Default, original)
	}
}

func TestErrCancelledIsDetectableViaErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("%w: at step 1 of 2 (Database password)", dialog.ErrCancelled)
	if !errors.Is(wrapped, dialog.ErrCancelled) {
		t.Errorf("errors.Is should match wrapped ErrCancelled")
	}
	if errors.Is(wrapped, dialog.ErrNoGUI) {
		t.Errorf("errors.Is should not match ErrNoGUI")
	}
}

func TestErrNoGUIIsDetectableViaErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("%w: SSH session detected", dialog.ErrNoGUI)
	if !errors.Is(wrapped, dialog.ErrNoGUI) {
		t.Errorf("errors.Is should match wrapped ErrNoGUI")
	}
}

func TestRecorderCapturesSpec(t *testing.T) {
	rec := &dialogtest.Recorder{
		PromptResults: []dialog.Result{{ID: "password", Value: "s3cret"}},
	}
	spec := dialog.Spec{
		Title: "agent-sql credential: foo",
		Items: []dialog.Field{{ID: "password", Label: "Password", InputType: dialog.Password}},
	}

	results, err := rec.Prompt(context.Background(), spec)
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(results) != 1 || results[0].Value != "s3cret" {
		t.Fatalf("results = %+v, want one result with value s3cret", results)
	}
	if len(rec.Calls) != 1 || rec.Calls[0].Title != spec.Title {
		t.Fatalf("recorded calls = %+v", rec.Calls)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCat    dialog.Category
		hintHasAny []string
	}{
		{"nil", nil, dialog.CategoryAgent, nil},
		{"cancelled", fmt.Errorf("%w (Database password)", dialog.ErrCancelled), dialog.CategoryRetry, []string{"cancel", "Re-run"}},
		{"no GUI", fmt.Errorf("%w: SSH session", dialog.ErrNoGUI), dialog.CategoryHuman, []string{"graphical desktop"}},
		{"unsupported", fmt.Errorf("%w: plan9", dialog.ErrUnsupported), dialog.CategoryHuman, []string{"graphical desktop"}},
		{"unknown", errors.New("something unrelated"), dialog.CategoryAgent, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, hint := dialog.ClassifyError(tc.err)
			if cat != tc.wantCat {
				t.Errorf("category = %q, want %q", cat, tc.wantCat)
			}
			for _, want := range tc.hintHasAny {
				if !strings.Contains(hint, want) {
					t.Errorf("hint = %q, want it to contain %q", hint, want)
				}
			}
		})
	}
}

func TestRecorderPropagatesAvailableErr(t *testing.T) {
	wantErr := fmt.Errorf("%w: testing", dialog.ErrNoGUI)
	rec := &dialogtest.Recorder{AvailableErr: wantErr}

	if err := rec.Available(); !errors.Is(err, dialog.ErrNoGUI) {
		t.Fatalf("Available() = %v, want wrapped ErrNoGUI", err)
	}
	_, err := rec.Prompt(context.Background(), dialog.Spec{})
	if !errors.Is(err, dialog.ErrNoGUI) {
		t.Fatalf("Prompt() = %v, want wrapped ErrNoGUI", err)
	}
}
