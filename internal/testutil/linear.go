package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// LinearIssueStub mirrors linear.Issue's JSON shape for fixture
// data. Pure data, so testutil does not import internal/linear and
// the linear package's own tests stay free to define their own
// fixtures inline. State is rendered as the nested `state.name`
// shape on the wire when the stub responds to viewer.assignedIssues
// queries. ID is the GraphQL node id used by the issueUpdate and
// commentCreate mutations; it stays empty on the list-issues path.
type LinearIssueStub struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       string `json:"-"`
}

// LinearProjectStub mirrors linear.Project's JSON shape. Same
// import-isolation rationale as LinearIssueStub.
type LinearProjectStub struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LinearStubResponses bundles the canned data a LinearStubServer
// returns. Issue may be nil to simulate a "issue not found"
// response (the GraphQL endpoint returns `data.issue: null`).
// AssignedIssues populates the viewer.assignedIssues response so
// picker tests can drive the issue-list flow without hand-rolling a
// second handler.
type LinearStubResponses struct {
	Issue          *LinearIssueStub
	AssignedIssues []LinearIssueStub
	Projects       []LinearProjectStub
	IssueErrors    []string
	HTTPStatus     int
	BodyOverride   string
}

// NewLinearStubServer returns a *httptest.Server that mimics the
// Linear GraphQL endpoint for the issue / projects queries `j`
// emits. Set linear.TestEndpoint to the returned URL before
// constructing a Client.
//
// The handler dispatches by substring match on the GraphQL query
// string: a body containing `issue(id:` returns Issue / IssueErrors;
// a body containing `projects` returns Projects. When responses.HTTPStatus
// is non-zero the handler returns that status with BodyOverride as
// the body so tests can exercise the 401 / non-2xx / malformed-JSON
// branches.
func NewLinearStubServer(
	responses LinearStubResponses,
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter, r *http.Request,
	) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if responses.HTTPStatus != 0 {
			w.WriteHeader(responses.HTTPStatus)
			_, _ = w.Write([]byte(responses.BodyOverride))
			return
		}
		query := string(body)
		if strings.Contains(query, "viewer{assignedIssues") {
			nodes := make([]map[string]any, 0, len(responses.AssignedIssues))
			for _, iss := range responses.AssignedIssues {
				nodes = append(nodes, map[string]any{
					"identifier": iss.Identifier,
					"title":      iss.Title,
					"url":        iss.URL,
					"state":      map[string]string{"name": iss.State},
				})
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{
						"assignedIssues": map[string]any{"nodes": nodes},
					},
				},
			})
			return
		}
		if strings.Contains(query, "issue(id:") {
			payload := map[string]any{
				"data": map[string]any{"issue": responses.Issue},
			}
			if len(responses.IssueErrors) > 0 {
				errs := make([]map[string]string, 0, len(responses.IssueErrors))
				for _, msg := range responses.IssueErrors {
					errs = append(errs, map[string]string{"message": msg})
				}
				payload["errors"] = errs
			}
			writeJSON(w, payload)
			return
		}
		if strings.Contains(query, "projects") {
			payload := map[string]any{
				"data": map[string]any{
					"projects": map[string]any{"nodes": responses.Projects},
				},
			}
			writeJSON(w, payload)
			return
		}
		http.Error(w, "stub: unknown query", http.StatusBadRequest)
	}))
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
