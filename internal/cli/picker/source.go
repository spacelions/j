package picker

import (
	"context"
	"errors"
	"fmt"
)

// Source is the planning input the user picks at the start of a
// new-or-resume flow. Values double as user-facing labels so the
// SelectSource picker and the cli's switch/case use one string
// constant. Each cli decides which subset to surface by passing
// `allowed` to SelectSource.
type Source string

const (
	SourceMarkdown Source = "markdown"
	SourceLinear   Source = "linear"
	SourceTask     Source = "re-plan an existing task"
)

// SelectSource renders the top-level source widget over the supplied
// allowed list. A returned Source is guaranteed to be one of `allowed`;
// an empty allowed list surfaces a wrapped error so misuse is loud at
// the call site.
func (p *Picker) SelectSource(ctx context.Context, allowed []Source) (Source, error) {
	if len(allowed) == 0 {
		return "", errors.New("picker: no sources allowed")
	}
	labels := make([]string, len(allowed))
	bySource := make(map[string]Source, len(allowed))
	for i, s := range allowed {
		labels[i] = string(s)
		bySource[string(s)] = s
	}
	chosen, err := p.choose(ctx, "Select plan source", labels)
	if err != nil {
		return "", err
	}
	got, ok := bySource[chosen]
	if !ok {
		return "", fmt.Errorf("picker: unknown source %q", chosen)
	}
	return got, nil
}
