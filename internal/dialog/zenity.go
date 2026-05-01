package dialog

import (
	"context"
	"errors"
	"fmt"

	"github.com/ncruces/zenity"
)

// zenityPrompter is the default backend. Sibling projects can replace it
// by implementing Prompter and calling SetDefault — nothing in the rest
// of the package depends on the zenity import.
type zenityPrompter struct{}

func (z *zenityPrompter) Available() error { return platformAvailable() }

func (z *zenityPrompter) Prompt(ctx context.Context, spec Spec) ([]Result, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	// Empty Items means "nothing to ask"; don't bother probing for a GUI.
	if len(spec.Items) == 0 {
		return nil, nil
	}
	if err := z.Available(); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(spec.Items))
	total := len(spec.Items)
	for i, item := range spec.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		title := stepTitle(spec.Title, i, total)
		value, err := promptOne(title, item)
		if err != nil {
			return nil, classifyZenityError(err, item)
		}
		results = append(results, Result{ID: item.ID, Value: value})
	}
	return results, nil
}

// stepTitle annotates the dialog title with "(step N of M)" when there
// is more than one field, so the user sees progress through the chain.
func stepTitle(base string, i, total int) string {
	if total <= 1 {
		return base
	}
	return fmt.Sprintf("%s (step %d of %d)", base, i+1, total)
}

// promptOne renders a single Field via zenity and returns the value.
// Behaviour-only — error classification belongs to the caller.
func promptOne(title string, item Field) (string, error) {
	switch item.InputType {
	case Password:
		// zenity.Password returns (username, password, err); single-field
		// mode discards the username return.
		_, value, err := zenity.Password(zenity.Title(title))
		return value, err
	case Text:
		return zenity.Entry(item.Label, zenity.Title(title))
	default:
		// Closed enum — reaching here is a programmer error.
		return "", fmt.Errorf("dialog: unsupported input type %d for field %q", item.InputType, item.ID)
	}
}

// classifyZenityError maps a raw zenity error onto our sentinel set,
// preserving the field label for context.
func classifyZenityError(err error, item Field) error {
	if errors.Is(err, zenity.ErrCanceled) {
		return fmt.Errorf("%w (%s)", ErrCancelled, item.Label)
	}
	return fmt.Errorf("dialog failed (%s): %w", item.Label, err)
}
