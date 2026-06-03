package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// TestEndpoint, when non-empty, replaces the per-host GraphQL
// endpoint inside NewClient. AGENTS.md "allowlist" hook used by tests
// to redirect traffic at a httptest.Server URL; production callers
// never read or write it. Restore the previous value with t.Cleanup
// so a failing test does not leak the override into the next case.
var TestEndpoint string

// JReplyMarker is the HTML comment embedded in every reply J posts to
// a review thread or conversation comment. The fetcher detects it
// when scanning prior replies so the planner can skip items J already
// answered. The marker is conservative: it lives inside an HTML
// comment so it never renders on github.com, and it is unique enough
// that human reviewers will not collide with it by accident.
const JReplyMarker = "<!-- j-code-review-reply -->"

// Client is the GitHub GraphQL client. Construct via NewClient; zero
// values are not usable. The client is stateless: every call rebuilds
// its HTTP request, threads PRRef.Endpoint() (or TestEndpoint), and
// returns parsed responses to the caller.
type Client struct {
	token string
	http  *http.Client
}

// Option configures a *Client at construction time. Currently only
// WithHTTPClient is exposed for tests that need to drive timeouts /
// transport behaviour; the production cli passes the token alone and
// relies on the defaults.
type Option func(*Client)

// WithHTTPClient overrides the http.Client used for outbound calls.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.http = httpClient }
}

// NewClient returns a *Client that authenticates with token (a
// classic personal access token or a fine-grained token). An empty
// token is allowed at construction; the do path returns
// ErrUnauthorized on the first call so the caller can short-circuit
// the prompt in one place.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token: token,
		http:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func firstGraphQLError(errs []graphQLError) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].Message
}

// do is the shared transport. Returns ErrUnauthorized on 401, an
// HTTPError on any other non-2xx status, and a wrapped json decode
// error on a 2xx body that does not match out's shape. Empty token
// short-circuits to ErrUnauthorized before any outbound call so the
// caller cannot mistake "no token" for "wrong token".
func (c *Client) do(
	ctx context.Context, endpoint string, req graphQLRequest, out any,
) error {
	if c.token == "" {
		return ErrUnauthorized
	}
	if TestEndpoint != "" {
		endpoint = TestEndpoint
	}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("github: decode: %w", err)
	}
	return nil
}
