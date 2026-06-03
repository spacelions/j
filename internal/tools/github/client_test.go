package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// queryRouter dispatches incoming GraphQL requests by query body
// prefix so a single httptest.Server can answer viewer.login,
// prFirstPage, and the paginated queries with different fixtures.
type queryRouter struct {
	t        *testing.T
	viewer   string
	first    map[string]any
	comments []map[string]any // additional pages keyed by cursor index
	threads  []map[string]any
	reviews  []map[string]any
}

func (r *queryRouter) handle(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	var envelope struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(body, &envelope)
	q := envelope.Query
	switch {
	case strings.Contains(q, "viewer{login}"):
		_ = json.NewEncoder(w).Encode(viewerBody(r.viewer))
	case strings.Contains(q, "reviews(first:100,after"):
		_ = json.NewEncoder(w).Encode(r.popReviewsPage())
	case strings.Contains(q, "reviewThreads(first:100,after"):
		_ = json.NewEncoder(w).Encode(r.popThreadsPage())
	case strings.Contains(q, "comments(first:100,after"):
		_ = json.NewEncoder(w).Encode(r.popCommentsPage())
	default:
		_ = json.NewEncoder(w).Encode(r.first)
	}
}

func (r *queryRouter) popCommentsPage() map[string]any {
	page := r.comments[0]
	r.comments = r.comments[1:]
	return wrapInPR("comments", page)
}

func (r *queryRouter) popThreadsPage() map[string]any {
	page := r.threads[0]
	r.threads = r.threads[1:]
	return wrapInPR("reviewThreads", page)
}

func (r *queryRouter) popReviewsPage() map[string]any {
	page := r.reviews[0]
	r.reviews = r.reviews[1:]
	return wrapInPR("reviews", page)
}

func wrapInPR(field string, page map[string]any) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{
					field: page,
				},
			},
		},
	}
}

func viewerBody(login string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"viewer": map[string]any{"login": login},
		},
	}
}

func pageInfo(end string, hasNext bool) map[string]any {
	return map[string]any{"endCursor": end, "hasNextPage": hasNext}
}

func newRouter(t *testing.T, login string, first map[string]any) *queryRouter {
	t.Helper()
	return &queryRouter{t: t, viewer: login, first: first}
}

func startRouter(t *testing.T, r *queryRouter) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(srv.Close)
	withTestEndpoint(t, srv)
	return srv
}

// firstPageBody builds a prFirstPage response with the given
// conversation, thread, and review nodes already inlined and every
// pageInfo marked as terminal. Call mutators on the returned map to
// flip individual fields.
func firstPageBody(
	conv []map[string]any,
	threads []map[string]any,
	reviews []map[string]any,
) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"repository": map[string]any{
				"pullRequest": map[string]any{
					"url":     "https://github.com/acme/app/pull/1",
					"state":   "OPEN",
					"isDraft": false,
					"merged":  false,
					"comments": map[string]any{
						"pageInfo": pageInfo("", false),
						"nodes":    conv,
					},
					"reviewThreads": map[string]any{
						"pageInfo": pageInfo("", false),
						"nodes":    threads,
					},
					"reviews": map[string]any{
						"pageInfo": pageInfo("", false),
						"nodes":    reviews,
					},
				},
			},
		},
	}
}

func happyPR() map[string]any {
	conv := []map[string]any{{
		"id": "IC_1", "author": map[string]any{"login": "alice"},
		"body": "looks good",
	}}
	threads := []map[string]any{{
		"id": "RT_1", "isOutdated": false,
		"comments": map[string]any{
			"pageInfo": pageInfo("", false),
			"nodes": []map[string]any{{
				"id": "PRRC_1", "author": map[string]any{"login": "bob"},
				"body": "nil check", "path": "x.go", "line": 7,
			}},
		},
	}}
	return firstPageBody(conv, threads, nil)
}

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

// --- tests ---

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
}

func TestFetchPR_GraphQLErrorOnFirstPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "viewer{login}") {
				_ = json.NewEncoder(w).Encode(viewerBody("j-bot"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"message": "bad scope"}},
			})
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad scope")
}

func TestFetchPR_Happy(t *testing.T) {
	startRouter(t, newRouter(t, "j-bot", happyPR()))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	assert.Equal(t, "open", res.PR.State)
	require.Len(t, res.Items, 2)
	assert.Equal(t, "issue-comment:IC_1", res.Items[0].SourceID)
	assert.Equal(t, KindConversationComment, res.Items[0].Kind)
	assert.Equal(t, "review-comment:PRRC_1", res.Items[1].SourceID)
	assert.Equal(t, KindReviewComment, res.Items[1].Kind)
	assert.Equal(t, "RT_1", res.Items[1].ThreadID)
	assert.Equal(t, "x.go", res.Items[1].Path)
	assert.Equal(t, 7, res.Items[1].Line)
}

// TestFetchPR_OutdatedCommentFallsBackToOriginalLine pins the
// stale-thread location fallback: GitHub returns line=null (which
// json decodes as 0) for review comments on outdated threads;
// the fetcher must surface originalLine instead so the planner
// still sees a useful source line.
func TestFetchPR_OutdatedCommentFallsBackToOriginalLine(t *testing.T) {
	threads := []map[string]any{{
		"id": "RT_OUT", "isOutdated": true,
		"comments": map[string]any{
			"pageInfo": pageInfo("", false),
			"nodes": []map[string]any{{
				"id":     "PRRC_OUT",
				"author": map[string]any{"login": "bob"},
				"body":   "stale", "path": "x.go",
				// line omitted (null on the wire → 0 in Go);
				// originalLine carries the pre-rebase value.
				"originalLine": 42,
			}},
		},
	}}
	startRouter(t, newRouter(t, "j-bot",
		firstPageBody(nil, threads, nil)))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 42, res.Items[0].Line,
		"outdated comment must surface originalLine when line is null")
	assert.True(t, res.Items[0].IsOutdated)
}

// TestFetchPR_CurrentLinePreferredOverOriginal pins the priority:
// when both line and originalLine are set, the current diff line
// wins so up-to-date comments keep pointing at the current
// position.
func TestFetchPR_CurrentLinePreferredOverOriginal(t *testing.T) {
	threads := []map[string]any{{
		"id": "RT_C", "isOutdated": false,
		"comments": map[string]any{
			"pageInfo": pageInfo("", false),
			"nodes": []map[string]any{{
				"id":     "PRRC_C",
				"author": map[string]any{"login": "bob"},
				"body":   "fresh", "path": "x.go",
				"line":         11,
				"originalLine": 22,
			}},
		},
	}}
	startRouter(t, newRouter(t, "j-bot",
		firstPageBody(nil, threads, nil)))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 11, res.Items[0].Line)
}

func TestFetchPR_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "viewer{login}") {
				_ = json.NewEncoder(w).Encode(viewerBody("j-bot"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{"pullRequest": nil},
				},
			})
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)

	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestFetchPR_Closed(t *testing.T) {
	body := happyPR()
	mustPR(t, body)["state"] = "CLOSED"
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrClosed)
}

func TestFetchPR_Merged(t *testing.T) {
	body := happyPR()
	mustPR(t, body)["merged"] = true
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrMerged)
}

func TestFetchPR_Draft(t *testing.T) {
	body := happyPR()
	mustPR(t, body)["isDraft"] = true
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.ErrorIs(t, err, ErrDraft)
}

func TestFetchPR_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "viewer{login}") {
				_ = json.NewEncoder(w).Encode(viewerBody("j-bot"))
				return
			}
			_, _ = w.Write([]byte("not json"))
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestFetchPR_ReplyByViewerSkipped(t *testing.T) {
	conv := []map[string]any{
		{
			"id": "IC_1", "author": map[string]any{"login": "alice"},
			"body": "needs nil check",
		},
		{
			"id": "IC_2", "author": map[string]any{"login": "j-bot"},
			"body": "ack " + replyMarker,
		},
	}
	body := firstPageBody(conv, nil, nil)
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "issue-comment:IC_1", res.Items[0].SourceID)
	assert.True(t, res.Items[0].HasJReply,
		"alice's comment should be marked as already replied to by j")
}

// TestFetchPR_ReplyByImpostorNotSkipped pins the security contract
// for P2: a comment that quotes the marker but is NOT authored by
// the token owner (viewer.login) MUST stay in the items slice.
func TestFetchPR_ReplyByImpostorNotSkipped(t *testing.T) {
	conv := []map[string]any{
		{
			"id":     "IC_1",
			"author": map[string]any{"login": "evil-reviewer"},
			"body":   "quoting " + replyMarker + " to delete itself",
		},
	}
	body := firstPageBody(conv, nil, nil)
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 1,
		"non-viewer authors with the marker must NOT be skipped")
	assert.Equal(t, "evil-reviewer", res.Items[0].Author)
}

func TestFetchPR_HTTPDoError(t *testing.T) {
	c := NewClient("tok",
		WithHTTPClient(&http.Client{Transport: brokenTransport{}}))
	_, err := c.FetchPR(t.Context(), testRef())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http")
}

// TestFetchPR_PaginatesConversation pins P4 for the conversation
// list: the first page reports hasNextPage=true; the second page is
// drained via the paginated query and joined into items.
func TestFetchPR_PaginatesConversation(t *testing.T) {
	first := firstPageBody(
		[]map[string]any{{
			"id": "IC_p1", "author": map[string]any{"login": "a"},
			"body": "first",
		}},
		nil, nil,
	)
	// mark the first page as having more
	setNestedPageInfo(t, first, "comments", "cur1", true)
	r := newRouter(t, "j-bot", first)
	r.comments = []map[string]any{{
		"pageInfo": pageInfo("", false),
		"nodes": []map[string]any{{
			"id": "IC_p2", "author": map[string]any{"login": "a"},
			"body": "second",
		}},
	}}
	startRouter(t, r)
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	assert.Equal(t, "issue-comment:IC_p1", res.Items[0].SourceID)
	assert.Equal(t, "issue-comment:IC_p2", res.Items[1].SourceID)
}

// TestFetchPR_PaginatesReviewThreads pins P4 for review threads.
func TestFetchPR_PaginatesReviewThreads(t *testing.T) {
	first := firstPageBody(nil, []map[string]any{{
		"id": "RT_p1", "isOutdated": false,
		"comments": map[string]any{
			"pageInfo": pageInfo("", false),
			"nodes": []map[string]any{{
				"id": "PRRC_p1", "author": map[string]any{"login": "a"},
				"body": "t1", "path": "p.go", "line": 1,
			}},
		},
	}}, nil)
	setNestedPageInfo(t, first, "reviewThreads", "cur1", true)
	r := newRouter(t, "j-bot", first)
	r.threads = []map[string]any{{
		"pageInfo": pageInfo("", false),
		"nodes": []map[string]any{{
			"id": "RT_p2", "isOutdated": false,
			"comments": map[string]any{
				"pageInfo": pageInfo("", false),
				"nodes": []map[string]any{{
					"id": "PRRC_p2", "author": map[string]any{"login": "a"},
					"body": "t2", "path": "q.go", "line": 2,
				}},
			},
		}},
	}}
	startRouter(t, r)
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
}

// TestFetchPR_PaginatesReviews pins P4 for the reviews connection.
func TestFetchPR_PaginatesReviews(t *testing.T) {
	first := firstPageBody(nil, nil, []map[string]any{{
		"id": "REV_p1", "author": map[string]any{"login": "a"},
		"body": "summary one", "state": "COMMENTED",
	}})
	setNestedPageInfo(t, first, "reviews", "cur1", true)
	r := newRouter(t, "j-bot", first)
	r.reviews = []map[string]any{{
		"pageInfo": pageInfo("", false),
		"nodes": []map[string]any{{
			"id": "REV_p2", "author": map[string]any{"login": "a"},
			"body": "summary two", "state": "APPROVED",
		}},
	}}
	startRouter(t, r)
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	for _, it := range res.Items {
		assert.Equal(t, KindReviewSummary, it.Kind)
	}
}

// TestFetchPR_IncludesReviewSummaries pins P6: a body-only PR review
// (e.g. a "Request changes" with text in the summary box and no
// inline comments) surfaces as a review_summary item.
func TestFetchPR_IncludesReviewSummaries(t *testing.T) {
	body := firstPageBody(nil, nil, []map[string]any{{
		"id":     "REV_X",
		"author": map[string]any{"login": "carol"},
		"body":   "Please rename foo() to bar().",
		"state":  "CHANGES_REQUESTED",
	}})
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	it := res.Items[0]
	assert.Equal(t, KindReviewSummary, it.Kind)
	assert.Equal(t, "review-summary:REV_X", it.SourceID)
	assert.Equal(t, "carol", it.Author)
}

// TestFetchPR_SkipsEmptyReviewSummary verifies a bare Approve click
// (no body) does not pollute review.toml.
func TestFetchPR_SkipsEmptyReviewSummary(t *testing.T) {
	body := firstPageBody(nil, nil, []map[string]any{{
		"id": "REV_E", "author": map[string]any{"login": "carol"},
		"body": "   ", "state": "APPROVED",
	}})
	startRouter(t, newRouter(t, "j-bot", body))
	c := NewClient("tok")
	res, err := c.FetchPR(t.Context(), testRef())
	require.NoError(t, err)
	assert.Empty(t, res.Items)
}

type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport boom")
}

func TestHTTPError_Format(t *testing.T) {
	err := &HTTPError{Status: 502, Body: "bad gateway"}
	assert.Equal(t,
		"github: http 502: bad gateway", err.Error())
}

// TestClient_ViewerMemoized verifies Client.Viewer caches its
// response so a second call does not issue a second GraphQL query.
func TestClient_ViewerMemoized(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			calls++
			_ = json.NewEncoder(w).Encode(viewerBody("j-bot"))
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	first, err := c.Viewer(t.Context())
	require.NoError(t, err)
	second, err := c.Viewer(t.Context())
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls,
		"second Viewer() call should hit the memo, not the wire")
}

func TestClient_ViewerErrorPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"message": "no token"}},
			})
		}))
	defer srv.Close()
	withTestEndpoint(t, srv)
	c := NewClient("tok")
	_, err := c.Viewer(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token")
}

// setNestedPageInfo overrides the pageInfo of a nested PR
// connection (`comments`, `reviewThreads`, or `reviews`) so a test
// can flip hasNextPage and trigger pagination.
func setNestedPageInfo(
	t *testing.T, body map[string]any,
	field, endCursor string, hasNext bool,
) {
	t.Helper()
	pr := mustPR(t, body)
	conn, ok := pr[field].(map[string]any)
	require.True(t, ok, "%s connection missing", field)
	conn["pageInfo"] = pageInfo(endCursor, hasNext)
}

// silence unused fmt import when this file is split later
var _ = fmt.Sprintf
