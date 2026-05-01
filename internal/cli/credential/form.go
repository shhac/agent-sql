package credential

import (
	"context"
	"fmt"

	"github.com/shhac/agent-sql/internal/dialog"
	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

// promptMissingViaDialog asks the user (via a native OS dialog) for any
// secret fields not supplied by --username / --password. Returns the
// (potentially filled-in) values.
//
// On any dialog failure, returns an *agenterrors.QueryError with the
// classification supplied by dialog.ClassifyError. The wrapped sentinel
// is preserved so callers can errors.Is downstream.
func promptMissingViaDialog(ctx context.Context, name, username, password string) (string, string, error) {
	spec, slots := buildCredentialSpec(name, &username, &password)
	if len(spec.Items) == 0 {
		return username, password, nil
	}

	if err := dialog.Default.Available(); err != nil {
		return username, password, classifyDialogErr(err, name)
	}

	results, err := dialog.Default.Prompt(ctx, spec)
	if err != nil {
		return username, password, classifyDialogErr(err, name)
	}

	applyResults(results, slots)
	return username, password, nil
}

// fieldSlot pairs a dialog.Field with the variable that should receive
// its value. Keeping them adjacent (built once, consumed once) removes
// the string-coupling between spec construction and result folding.
type fieldSlot struct {
	field dialog.Field
	dest  *string
}

// buildCredentialSpec assembles the dialog Spec for any blank credential
// fields. The returned slots have the same length as Spec.Items and
// share the order; applyResults walks them in lockstep.
func buildCredentialSpec(name string, username, password *string) (dialog.Spec, []fieldSlot) {
	candidates := []fieldSlot{
		{
			field: dialog.Field{ID: "username", Label: "Database username", InputType: dialog.Text},
			dest:  username,
		},
		{
			field: dialog.Field{ID: "password", Label: "Database password", InputType: dialog.Password},
			dest:  password,
		},
	}
	slots := make([]fieldSlot, 0, len(candidates))
	items := make([]dialog.Field, 0, len(candidates))
	for _, c := range candidates {
		if *c.dest != "" {
			continue
		}
		slots = append(slots, c)
		items = append(items, c.field)
	}
	return dialog.Spec{
		Title: fmt.Sprintf("agent-sql credential: %s", name),
		Items: items,
	}, slots
}

// applyResults writes each Result's Value into the slot's destination by
// matching field ID. Order is preserved so an i-by-i walk works, but we
// match by ID for safety against future spec rearrangement.
func applyResults(results []dialog.Result, slots []fieldSlot) {
	byID := make(map[string]*string, len(slots))
	for _, s := range slots {
		byID[s.field.ID] = s.dest
	}
	for _, r := range results {
		if dest, ok := byID[r.ID]; ok {
			*dest = r.Value
		}
	}
}

// classifyDialogErr is the agent-sql adapter from a dialog package error
// to our QueryError envelope. Sibling projects re-implement this in ~10
// lines for their own error type — the heavy lifting (sentinel→category)
// is in dialog.ClassifyError so the mapping itself doesn't drift.
func classifyDialogErr(err error, name string) error {
	cat, hint := dialog.ClassifyError(err)

	// Augment the generic hint with agent-sql-specific guidance.
	switch cat {
	case dialog.CategoryHuman:
		hint = "agent-sql credential add --form requires a graphical desktop session. " +
			"Ask the user to run on their local machine, or fall back to non-interactive: " +
			fmt.Sprintf("agent-sql credential add %s --username <u> --password <secret>", name)
	case dialog.CategoryRetry:
		hint = "User cancelled the dialog. Re-run agent-sql credential add --form to retry."
	}

	return agenterrors.Wrap(err, categoryToFixableBy(cat)).WithHint(hint)
}

// categoryToFixableBy bridges dialog's neutral Category to agent-sql's
// FixableBy enum. The two are isomorphic; this is a one-line mapping.
func categoryToFixableBy(c dialog.Category) agenterrors.FixableBy {
	switch c {
	case dialog.CategoryHuman:
		return agenterrors.FixableByHuman
	case dialog.CategoryRetry:
		return agenterrors.FixableByRetry
	default:
		return agenterrors.FixableByAgent
	}
}
