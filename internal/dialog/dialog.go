// Package dialog provides a small abstraction over native OS dialogs for
// LLM-safe credential entry. The LLM driving the CLI never sees what the
// user types — input goes directly into the OS, and only a redacted
// receipt comes back over stdout.
//
// # Usage
//
// Callers use the package-level [Default] Prompter. Tests can swap it
// via [SetDefault] and restore in t.Cleanup.
//
// # Backend
//
// The default backend wraps github.com/ncruces/zenity. It is the only
// place this package depends on zenity — sibling projects can replace
// the default with their own [Prompter] (e.g. a Win32 native popup, a
// localhost browser form, or a stub) without touching anything else.
//
// # Availability contract
//
// [Prompter.Available] returns nil when a GUI dialog can plausibly be
// shown. It returns an error wrapping [ErrNoGUI] when the host is
// clearly headless (no display server, no SSH local-terminal hint, no
// interactive Windows session) and an error wrapping [ErrUnsupported]
// on platforms with no implementation. Available is best-effort — when
// it cannot pre-classify, it returns nil and lets the dialog itself
// surface a platform-specific error from [Prompter.Prompt].
//
// # Error classification
//
// [ClassifyError] maps Prompter errors onto a neutral [Category] +
// hint string. Sibling projects plug this into their own error envelope
// (e.g. {"error": err.Error(), "fixable_by": cat, "hint": hint}) without
// re-deriving the sentinel→category mapping.
package dialog

import (
	"context"
	"errors"
	"fmt"
)

// InputType is the kind of field requested.
type InputType int

const (
	// Text is a plain text entry field.
	Text InputType = iota
	// Password is a hidden entry field (echo off).
	Password
)

// Field is a single input requested from the user.
type Field struct {
	ID        string
	Label     string
	InputType InputType
}

// Spec is a complete dialog request.
type Spec struct {
	Title string
	Items []Field
}

// Result is one filled-in field.
type Result struct {
	ID    string
	Value string
}

// Sentinel errors returned by Prompter implementations. Callers use
// errors.Is to classify failures.
var (
	ErrCancelled   = errors.New("cancelled by user")
	ErrNoGUI       = errors.New("no GUI dialog available")
	ErrUnsupported = errors.New("platform unsupported")
)

// Category groups Prompter errors by who can fix them. Mirrors common
// LLM-error taxonomies (e.g. agent-sql's fixable_by) but stays free of
// host-application types so this package is droppable into siblings.
type Category string

const (
	// CategoryHuman — environment issue. The user must change something
	// (use a graphical machine, install zenity/kdialog, etc.). Don't retry.
	CategoryHuman Category = "human"
	// CategoryRetry — transient. The user cancelled the dialog; re-running
	// the same command is the right next step.
	CategoryRetry Category = "retry"
	// CategoryAgent — anything else. The agent can probably correct it
	// (bad spec, programmer error, unknown InputType, etc.).
	CategoryAgent Category = "agent"
)

// ClassifyError maps a Prompter error onto a Category and a hint string
// suitable for surfacing to an LLM. Returns (CategoryAgent, "") for nil
// or unrecognised errors so callers can treat the result uniformly.
//
// Sibling projects can plug this directly into their own error envelope
// (e.g. {"error": err.Error(), "fixable_by": cat, "hint": hint}) without
// re-deriving the sentinel→category mapping.
func ClassifyError(err error) (Category, string) {
	switch {
	case err == nil:
		return CategoryAgent, ""
	case errors.Is(err, ErrCancelled):
		return CategoryRetry, "User cancelled the dialog. Re-run to retry."
	case errors.Is(err, ErrNoGUI), errors.Is(err, ErrUnsupported):
		return CategoryHuman, "A graphical desktop session is required. Run on the user's local machine, or fall back to a non-interactive flow."
	default:
		return CategoryAgent, ""
	}
}

// Prompter renders a Spec to the user and returns their answers.
//
// Implementations must:
//   - Return Result entries in the same order as Spec.Items.
//   - Return an error wrapping ErrCancelled if the user dismisses any popup.
//   - Return an error wrapping ErrNoGUI if Available reports the host is unusable.
type Prompter interface {
	Prompt(ctx context.Context, spec Spec) ([]Result, error)
	Available() error
}

// Default is the Prompter used by the CLI. Tests swap it via SetDefault.
var Default Prompter = &zenityPrompter{}

// SetDefault replaces the package-level Default and returns a function
// that restores the previous value. Intended for tests.
func SetDefault(p Prompter) (restore func()) {
	prev := Default
	Default = p
	return func() { Default = prev }
}

// validateSpec checks Spec invariants that don't depend on a backend.
// An empty Items list is allowed (returns nil) — Prompter implementations
// short-circuit on it. An unknown InputType is rejected up-front so the
// error is the same regardless of which backend is configured.
func validateSpec(spec Spec) error {
	for _, item := range spec.Items {
		if item.InputType != Text && item.InputType != Password {
			return fmt.Errorf("dialog: invalid InputType %d for field %q", item.InputType, item.ID)
		}
	}
	return nil
}
