package picker

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/tools/linear"
)

// PromptLinearAPIKey opens openURL in the user's default browser
// (best-effort) and prompts for the personal Linear API token. A
// best-effort failure to launch the browser is silent — the input
// description echoes the URL so the user can paste it into a browser
// manually. A user abort (Ctrl-C / Esc) propagates as an error so
// the caller's CleanAbort guard converts it to a nil return.
func (p *Picker) PromptLinearAPIKey(
	ctx context.Context, openURL string,
) (string, bool, error) {
	_ = linear.OpenURL(openURL)
	var token string
	if err := p.run(ctx, huh.NewInput().
		Title("Paste your Linear API key").
		Description(fmt.Sprintf("Open %s to create one", openURL)).
		Value(&token)); err != nil {
		return "", false, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// PickLinearProject renders a single-select widget over projects and
// returns the chosen entry. An abort (Ctrl-C / Esc) propagates as an
// error so the caller's CleanAbort guard converts it to a nil return;
// an empty project list yields ok=false with no UI driven so the
// caller can fall through to the identifier prompt without saving a
// project.
func (p *Picker) PickLinearProject(
	ctx context.Context, projects []linear.Project,
) (linear.Project, bool, error) {
	if len(projects) == 0 {
		return linear.Project{}, false, nil
	}
	labels := make([]string, len(projects))
	byLabel := make(map[string]linear.Project, len(projects))
	for i, prj := range projects {
		label := prj.Name
		if label == "" {
			label = prj.ID
		}
		labels[i] = label
		byLabel[label] = prj
	}
	chosen, err := p.choose(ctx, "Select default Linear project", labels)
	if err != nil {
		return linear.Project{}, false, err
	}
	return byLabel[chosen], true, nil
}

// PickLinearIssue renders a single-select widget over the supplied
// issues and returns the chosen entry. The label format —
// `ENG-123 — <state> — <title>` — mirrors PickTask
// (internal/cli/picker/task.go:46) so the source picker reads
// consistently across markdown / linear / existing-task branches.
//
// Empty list short-circuits with ok=false (no UI driven); the
// caller (pickLinearSource) catches that earlier and surfaces a
// clear error.
//
// Abort (Ctrl-C / Esc) propagates as an error so the caller's
// CleanAbort guard converts it to a nil return.
func (p *Picker) PickLinearIssue(
	ctx context.Context, issues []linear.Issue,
) (linear.Issue, bool, error) {
	if len(issues) == 0 {
		return linear.Issue{}, false, nil
	}
	labels := make([]string, len(issues))
	byLabel := make(map[string]linear.Issue, len(issues))
	for i, iss := range issues {
		title := strings.TrimSpace(iss.Title)
		if title == "" {
			title = "(no title)"
		}
		state := strings.TrimSpace(iss.State)
		if state == "" {
			state = "(no state)"
		}
		label := fmt.Sprintf("%s — %s — %s", iss.Identifier, state, title)
		labels[i] = label
		byLabel[label] = iss
	}
	chosen, err := p.choose(ctx, "Select a Linear issue", labels)
	if err != nil {
		return linear.Issue{}, false, err
	}
	return byLabel[chosen], true, nil
}
