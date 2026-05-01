// Package dialogtest provides a recording fake [dialog.Prompter] for use
// in tests. Callers swap it via dialog.SetDefault and inspect the spec(s)
// the CLI sent to the dialog system.
package dialogtest

import (
	"context"

	"github.com/shhac/agent-sql/internal/dialog"
)

// Recorder is a dialog.Prompter that returns canned responses and records
// the Spec values it was called with.
type Recorder struct {
	// AvailableErr, if non-nil, is returned from Available() and Prompt()
	// (Prompt mirrors the contract: it must check Available first).
	AvailableErr error
	// PromptResults are returned from Prompt() when AvailableErr is nil
	// and PromptErr is nil.
	PromptResults []dialog.Result
	// PromptErr, if non-nil, is returned from Prompt() instead of results.
	PromptErr error

	// Calls records every Spec passed to Prompt, in order.
	Calls []dialog.Spec
}

// Available implements dialog.Prompter.
func (r *Recorder) Available() error { return r.AvailableErr }

// Prompt implements dialog.Prompter.
func (r *Recorder) Prompt(_ context.Context, spec dialog.Spec) ([]dialog.Result, error) {
	r.Calls = append(r.Calls, spec)
	if r.AvailableErr != nil {
		return nil, r.AvailableErr
	}
	if r.PromptErr != nil {
		return nil, r.PromptErr
	}
	return r.PromptResults, nil
}
