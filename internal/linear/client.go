package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultEndpoint is the production GraphQL endpoint. Tests inject a
// httptest.Server URL via WithEndpoint instead of stubbing the whole
// http.RoundTripper, which keeps the public client allowlist-friendly
// per AGENTS.md.
const DefaultEndpoint = "https://api.linear.app/graphql"

// LinearAPIKeysURL is the personal-API-keys page the link prompt
// opens in the user's browser. It is a constant so tests and the
// picker share the same value.
const LinearAPIKeysURL = "https://linear.app/settings/api"

// TestEndpoint, when non-empty, replaces DefaultEndpoint inside
// NewClient. AGENTS.md "allowlist" hook used by tests to redirect
// traffic at a httptest.Server URL; production callers never read
// or write it. Restore the previous value with t.Cleanup so a
// failing test does not leak the override into the next case.
var TestEndpoint string

// Client is the GraphQL client. Construct via NewClient; zero values
// are not usable.
type Client struct {
	apiKey   string
	endpoint string
	http     *http.Client
}

// Option configures a *Client at construction time. Used only by the
// client and its tests; callers in the cli pass an apiKey and rely on
// the defaults.
type Option func(*Client)

// WithEndpoint overrides the GraphQL endpoint. Tests point the client
// at their httptest.Server URL.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = endpoint }
}

// WithHTTPClient overrides the http.Client used for outbound calls.
// Tests rarely need this — WithEndpoint is enough to redirect traffic
// to a httptest.Server — but a custom transport is exposed for
// timeout / retry tests if they appear.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.http = httpClient }
}

// NewClient returns a *Client that authenticates with apiKey. The
// endpoint defaults to DefaultEndpoint (or the test-only
// TestEndpoint override when set) and the underlying http.Client
// defaults to http.DefaultClient; both are overridable via options
// for callers that need a fully isolated round-trip.
func NewClient(apiKey string, opts ...Option) *Client {
	endpoint := DefaultEndpoint
	if TestEndpoint != "" {
		endpoint = TestEndpoint
	}
	c := &Client{
		apiKey:   apiKey,
		endpoint: endpoint,
		http:     http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetIssue fetches a single issue by its `<TEAM>-<NUM>` identifier.
// Pre-flight: ValidateIdentifier rejects malformed input before the
// GraphQL round-trip so a 400 is never returned for "foo". A null
// `issue` node maps to ErrNotFound (wrapped with the identifier); a
// `errors[]` array surfaces as a wrapped error carrying the first
// message.
func (c *Client) GetIssue(ctx context.Context, id string) (Issue, error) {
	if err := ValidateIdentifier(id); err != nil {
		return Issue{}, err
	}
	var resp issueResponse
	req := graphQLRequest{
		Query:     issueQuery,
		Variables: map[string]any{"id": id},
	}
	if err := c.do(ctx, req, &resp); err != nil {
		return Issue{}, err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return Issue{}, fmt.Errorf("linear: %s", msg)
	}
	if resp.Data.Issue == nil {
		return Issue{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return *resp.Data.Issue, nil
}

// ListProjects returns every Linear project the API key can see, in
// the order the GraphQL response provides. The Linear API gates this
// list per-token so the result is the user's own projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var resp projectsResponse
	req := graphQLRequest{Query: projectsQuery}
	if err := c.do(ctx, req, &resp); err != nil {
		return nil, err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return nil, fmt.Errorf("linear: %s", msg)
	}
	return resp.Data.Projects.Nodes, nil
}

// ListAssignedIssues returns the API-key-owner's assigned, open
// issues — at most 50, ordered by `updatedAt desc`. Issues in
// completed/canceled states are filtered server-side via Linear's
// `state.type` enum so closed work never reaches the picker. When
// opts.ProjectID is non-empty, the query additionally restricts
// results to issues whose project.id equals it.
//
// Description is intentionally not requested; the list view is for
// picking, and the per-issue body is fetched on demand by GetIssue
// once the user picks one.
func (c *Client) ListAssignedIssues(
	ctx context.Context, opts ListIssuesOpts,
) ([]Issue, error) {
	req := graphQLRequest{Query: assignedIssuesQuery}
	if opts.ProjectID != "" {
		req = graphQLRequest{
			Query:     assignedIssuesByProjectQuery,
			Variables: map[string]any{"projectId": opts.ProjectID},
		}
	}
	var resp assignedIssuesResponse
	if err := c.do(ctx, req, &resp); err != nil {
		return nil, err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return nil, fmt.Errorf("linear: %s", msg)
	}
	nodes := resp.Data.Viewer.AssignedIssues.Nodes
	out := make([]Issue, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, Issue{
			Identifier: n.Identifier,
			Title:      n.Title,
			URL:        n.URL,
			State:      n.State.Name,
		})
	}
	return out, nil
}

// do is the shared transport: marshals req, POSTs to the endpoint
// with the API key in the Authorization header, maps 401 to
// ErrUnauthorized, wraps every other non-2xx as *HTTPError, and
// decodes a 2xx body into out.
func (c *Client) do(ctx context.Context, req graphQLRequest, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("linear: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("linear: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("linear: read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("linear: decode: %w", err)
	}
	return nil
}

