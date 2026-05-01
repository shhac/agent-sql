package dialog

import (
	"errors"
	"testing"
)

func TestStepTitle(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		i     int
		total int
		want  string
	}{
		{"single field — no annotation", "agent-sql credential: foo", 0, 1, "agent-sql credential: foo"},
		{"first of two — annotated", "title", 0, 2, "title (step 1 of 2)"},
		{"second of two — annotated", "title", 1, 2, "title (step 2 of 2)"},
		{"third of three", "title", 2, 3, "title (step 3 of 3)"},
		{"zero total — no annotation (defensive)", "title", 0, 0, "title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stepTitle(tc.base, tc.i, tc.total)
			if got != tc.want {
				t.Errorf("stepTitle(%q, %d, %d) = %q, want %q", tc.base, tc.i, tc.total, got, tc.want)
			}
		})
	}
}

func TestValidateSpec(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		wantErr bool
	}{
		{"empty items — allowed", Spec{Title: "x"}, false},
		{"all valid types", Spec{Items: []Field{
			{ID: "a", InputType: Text},
			{ID: "b", InputType: Password},
		}}, false},
		{"unknown type rejected", Spec{Items: []Field{
			{ID: "a", InputType: InputType(99)},
		}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSpec(tc.spec)
			if tc.wantErr && err == nil {
				t.Errorf("validateSpec() error = nil, want non-nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateSpec() error = %v, want nil", err)
			}
		})
	}
}

func TestZenityPromptEmptyItemsReturnsNoError(t *testing.T) {
	z := &zenityPrompter{}
	results, err := z.Prompt(t.Context(), Spec{Title: "x"})
	if err != nil {
		t.Fatalf("Prompt(empty) error = %v, want nil", err)
	}
	if len(results) != 0 {
		t.Errorf("Prompt(empty) results = %v, want empty", results)
	}
}

func TestZenityPromptRejectsUnknownInputType(t *testing.T) {
	z := &zenityPrompter{}
	_, err := z.Prompt(t.Context(), Spec{
		Title: "x",
		Items: []Field{{ID: "a", InputType: InputType(99)}},
	})
	if err == nil {
		t.Fatal("Prompt(invalid InputType) error = nil, want non-nil")
	}
}

func TestClassifyZenityErrorPreservesCancelSentinel(t *testing.T) {
	// Use a fake error that errors.Is treats as zenity.ErrCanceled would
	// be treated. We can't easily synthesize zenity.ErrCanceled itself
	// without invoking zenity, but we can test classifyZenityError's
	// fallback path here and rely on the integration test (recorder
	// returning ErrCancelled) to cover the cancellation arm.
	wrapped := classifyZenityError(errors.New("connection failed"), Field{Label: "Database password"})
	if wrapped == nil {
		t.Fatal("classifyZenityError returned nil for non-nil input")
	}
	// Should NOT be classified as cancelled.
	if errors.Is(wrapped, ErrCancelled) {
		t.Errorf("classifyZenityError mis-classified random error as cancelled")
	}
}
