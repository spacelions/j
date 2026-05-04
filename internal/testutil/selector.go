package testutil

import "context"

// SelectorFake satisfies picker.Selector (SelectTool + SelectModel)
// for cli tests. plan / work / verify embed it inside their own
// scriptedUI; tasks/agentcheck uses it directly. The zero value
// returns the first option for both prompts so most tests need
// only set the fields they assert on.
type SelectorFake struct {
	Tool     string
	Model    string
	ToolErr  error
	ModelErr error

	ToolCalls  int
	ModelCalls int

	// LastTools / LastModels record the options slice passed to the
	// last call so tests can assert on the ordered list rendered to
	// the user.
	LastTools  []string
	LastModels []string

	// ToolHook, when non-nil, runs at the start of SelectTool so
	// tests can mutate shared state (e.g. close an injected store)
	// between Pick and the post-Pick persist step.
	ToolHook func()
}

func (s *SelectorFake) SelectTool(_ context.Context, options []string) (string, error) {
	s.ToolCalls++
	s.LastTools = append([]string(nil), options...)
	if s.ToolHook != nil {
		s.ToolHook()
	}
	if s.ToolErr != nil {
		return "", s.ToolErr
	}
	if s.Tool != "" {
		return s.Tool, nil
	}
	return options[0], nil
}

func (s *SelectorFake) SelectModel(_ context.Context, options []string) (string, error) {
	s.ModelCalls++
	s.LastModels = append([]string(nil), options...)
	if s.ModelErr != nil {
		return "", s.ModelErr
	}
	if s.Model != "" {
		return s.Model, nil
	}
	return options[0], nil
}
