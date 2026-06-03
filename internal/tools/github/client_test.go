package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTestEndpoint redirects every NewClient call to srv.URL until
// the test cleans up.
func withTestEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := TestEndpoint
	TestEndpoint = srv.URL
	t.Cleanup(func() { TestEndpoint = prev })
}

func testRef() PRRef {
	return PRRef{
		Host: "github.com", Owner: "acme", Repo: "app", Number: 1,
	}
}

func TestFetchPR_NoToken(t *testing.T) {
	c := NewClient("")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestFetchPR_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestFetchPR_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	var httpErr *HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 500, httpErr.Status)
	assert.Contains(t, httpErr.Error(), "500")
}

func TestFetchPR_GraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			body := map[string]any{
				"errors": []map[string]string{{"message": "bad scope"}},
			}
			_ = json.NewEncoder(w).Encode(body)
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad scope")
}

// happyPRResponse returns a JSON body that mirrors the GraphQL
// response shape with one conversation comment and one review thread.
func happyPRResponse() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{
					"url":     "https://github.com/acme/app/pull/1",
					"state":   "OPEN",
					"isDraft": false,
					"merged":  false,
					"comments": map[string]any{
						"nodes": []map[string]any{
							{
								"databaseId": 11,
								"author":     map[string]any{"login": "alice"},
								"body":       "looks good",
							},
						},
					},
					"reviewThreads": map[string]any{
						"nodes": []map[string]any{
							{
								"id":         "thread1",
								"isOutdated": false,
								"comments": map[string]any{
									"nodes": []map[string]any{
										{
											"databaseId": 22,
											"author":     map[string]any{"login": "bob"},
											"body":       "nil check",
											"path":       "x.go",
											"line":       7,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestFetchPR_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			raw, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(raw), `"owner":"acme"`)
			_ = json.NewEncoder(w).Encode(happyPRResponse())
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	assert.Equal(t, "open", res.PR.State)
	require.Len(t, res.Items, 2)
	assert.Equal(t, "issue-comment:11", res.Items[0].SourceID)
	assert.Equal(t, KindConversationComment, res.Items[0].Kind)
	assert.Equal(t, "review-comment:22", res.Items[1].SourceID)
	assert.Equal(t, KindReviewComment, res.Items[1].Kind)
	assert.Equal(t, "thread1", res.Items[1].ThreadID)
	assert.Equal(t, "x.go", res.Items[1].Path)
	assert.Equal(t, 7, res.Items[1].Line)
}

func TestFetchPR_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			body := map[string]any{
				"data": map[string]any{
					"repository": map[string]any{"pullRequest": nil},
				},
			}
			_ = json.NewEncoder(w).Encode(body)
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFetchPR_Closed(t *testing.T) {
	body := happyPRResponse()
	pr := mustPR(t, body)
	pr["state"] = "CLOSED"
	srv := newHandler(t, body)
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrClosed)
}

func TestFetchPR_Merged(t *testing.T) {
	body := happyPRResponse()
	pr := mustPR(t, body)
	pr["merged"] = true
	srv := newHandler(t, body)
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrMerged)
}

func TestFetchPR_Draft(t *testing.T) {
	body := happyPRResponse()
	pr := mustPR(t, body)
	pr["isDraft"] = true
	srv := newHandler(t, body)
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrDraft)
}

func TestFetchPR_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestFetchPR_JReplySkipped(t *testing.T) {
	body := happyPRResponse()
	pr := mustPR(t, body)
	comments, ok := pr["comments"].(map[string]any)
	require.True(t, ok)
	nodes, ok := comments["nodes"].([]map[string]any)
	require.True(t, ok)
	comments["nodes"] = append(nodes, map[string]any{
		"databaseId": 33,
		"author":     map[string]any{"login": "j-bot"},
		"body":       "ack " + JReplyMarker,
	})
	srv := newHandler(t, body)
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	assert.Len(t, res.Items, 2) // j reply skipped
	for _, it := range res.Items {
		assert.NotContains(t, it.Body, JReplyMarker)
	}
	assert.True(t, res.Items[0].HasJReply,
		"alice's comment should be flagged as already having a j reply")
}

func TestFetchPR_HTTPDoError(t *testing.T) {
	c := NewClient("tok",
		WithHTTPClient(&http.Client{Transport: brokenTransport{}}))
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http")
}

type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

func newHandler(t *testing.T, body map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(body)
		}))
}

// mustPR walks the happyPRResponse map into the pullRequest object
// and aborts the test on any unexpected shape.
func mustPR(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	repo, ok := data["repository"].(map[string]any)
	require.True(t, ok)
	pr, ok := repo["pullRequest"].(map[string]any)
	require.True(t, ok)
	return pr
}

func TestHTTPError_Format(t *testing.T) {
	err := &HTTPError{Status: 502, Body: "bad gateway"}
	assert.Equal(t,
		"github: http 502: bad gateway", err.Error())
	assert.Contains(t, err.Error(), "502")
}
