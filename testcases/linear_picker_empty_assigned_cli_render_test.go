package testcases_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/spacelions/j/internal/cli/picker"
	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/linear"
	"github.com/spacelions/j/internal/store"
)

// TestLinearPickerEmptyAssignedCLIRender pins SPA-54's CLI surfacing:
// when the picker's empty-assigned-issues branch fires, the error
// reaches the cli.Execute print boundary that root.go uses
// (`uitheme.DangerousFprintf(os.Stderr, "J: %v\n", err)`) and the
// user-visible payload, with ANSI styling stripped, is exactly
// `J: no Linear issues assigned to you\n`.
//
// Black-box: drives picker.PickSource against a stubbed empty-list
// Linear endpoint, then formats the error through the same helper
// the root cobra wrapper uses, so the assertion mirrors what a real
// user sees on stderr.
func TestLinearPickerEmptyAssignedCLIRender(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	if err := linear.SaveAPIKey("lin_api_test"); err != nil {
		t.Fatalf("SaveAPIKey: %v", err)
	}
	if err := linear.SaveProject("p"); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{
						"assignedIssues": map[string]any{
							"nodes": []any{},
						},
					},
				},
			})
		}))
	t.Cleanup(srv.Close)
	prev := linear.TestEndpoint
	linear.TestEndpoint = srv.URL
	t.Cleanup(func() { linear.TestEndpoint = prev })

	ui := &emptyAssignedIssuesUI{}
	_, err := picker.PickSource(
		t.Context(), ui,
		[]picker.Source{picker.SourceLinear}, nil, nil,
	)
	if err == nil {
		t.Fatalf("err = nil, want empty-list error from picker")
	}

	var buf bytes.Buffer
	if _, fpErr := uitheme.DangerousFprintf(
		&buf, "J: %v\n", err,
	); fpErr != nil {
		t.Fatalf("DangerousFprintf: %v", fpErr)
	}

	stripped := ansi.Strip(buf.String())
	const want = "J: no Linear issues assigned to you\n"
	if stripped != want {
		t.Fatalf("stripped CLI render = %q, want %q", stripped, want)
	}
	if strings.Contains(stripped, "J: J:") { //nolint:dupword // intentionally checking for a double-prefix bug
		t.Fatalf("stripped CLI render has double J: prefix: %q",
			stripped)
	}
	if strings.Contains(stripped, "picker:") {
		t.Fatalf("stripped CLI render leaks picker: prefix: %q",
			stripped)
	}
}
