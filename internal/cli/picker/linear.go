package picker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/spacelions/j/internal/linear"
)

// PromptLinearAPIKey opens openURL in the user's default browser
// (best-effort) and prompts for the personal Linear API token. A
// best-effort failure to launch the browser is silent — the input
// description echoes the URL so the user can paste it into a browser
// manually. A user abort (Ctrl-C / Esc) returns ok=false with a nil
// error so the caller exits the link flow cleanly.
func (p *Picker) PromptLinearAPIKey(ctx context.Context, openURL string) (string, bool, error) {
	_ = linear.OpenURL(openURL)
	var token string
	err := p.run(ctx, huh.NewInput().
		Title("Paste your Linear API key").
		Description(fmt.Sprintf("Open %s to create one (lin_api_…)", openURL)).
		EchoMode(huh.EchoModePassword).
		Value(&token))
	if errors.Is(err, huh.ErrUserAborted) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// PickLinearProject renders a single-select widget over projects and
// returns the chosen entry. An abort (Ctrl-C / Esc) returns ok=false
// with a nil error; an empty project list yields ok=false too so the
// caller can fall through to the identifier prompt without saving a
// project.
func (p *Picker) PickLinearProject(ctx context.Context, projects []linear.Project) (linear.Project, bool, error) {
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
	if errors.Is(err, huh.ErrUserAborted) {
		return linear.Project{}, false, nil
	}
	if err != nil {
		return linear.Project{}, false, err
	}
	prj, ok := byLabel[chosen]
	if !ok {
		return linear.Project{}, false, fmt.Errorf("picker: unknown project selection %q", chosen)
	}
	return prj, true, nil
}

// PromptLinearIdentifier prompts for the issue identifier (e.g.
// `ENG-123`). The huh.Validate hook calls linear.ValidateIdentifier
// so a typo surfaces inside the form instead of after the round-trip.
// Abort returns ok=false with nil error.
func (p *Picker) PromptLinearIdentifier(ctx context.Context) (string, bool, error) {
	var id string
	err := p.run(ctx, huh.NewInput().
		Title("Linear issue identifier").
		Description("e.g. ENG-123").
		Validate(func(s string) error {
			return linear.ValidateIdentifier(strings.TrimSpace(s))
		}).
		Value(&id))
	if errors.Is(err, huh.ErrUserAborted) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(id), true, nil
}
