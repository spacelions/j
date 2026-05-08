package picker

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
)

// ConfirmStatusOverride renders a yes/no prompt when the resolved
// task's status falls outside the cli's natural allowlist. cmd is the
// command label rendered into the prompt ("plan", "work", "verify",
// "re-plan"); taskID and status come from the row. The default answer
// is "no" so a stray Enter does not run the agent against a task
// that's still in flight or already past the relevant phase.
//
// huh.ErrUserAborted is propagated verbatim and the cli's deferred
// guard converts it to a nil return — same contract as the rest of
// the picker leaves.
func (p *Picker) ConfirmStatusOverride(
	ctx context.Context, cmd, taskID, status string,
) (bool, error) {
	title := fmt.Sprintf(
		"Task %s is in status %s; %s anyway?", taskID, status, cmd,
	)
	v := false
	if err := p.run(ctx, huh.NewConfirm().
		Title(title).
		Affirmative("yes").
		Negative("no").
		Value(&v)); err != nil {
		return false, err
	}
	return v, nil
}
