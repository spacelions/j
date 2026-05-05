package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func issueServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("k")
	if c.endpoint != DefaultEndpoint {
		t.Fatalf("endpoint = %q, want %q", c.endpoint, DefaultEndpoint)
	}
	if c.http != http.DefaultClient {
		t.Fatal("http client should default to http.DefaultClient")
	}
}

func TestNewClient_WithEndpoint(t *testing.T) {
	c := NewClient("k", WithEndpoint("https://example/graphql"))
	if c.endpoint != "https://example/graphql" {
		t.Fatalf("endpoint = %q", c.endpoint)
	}
}

func TestNewClient_WithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c := NewClient("k", WithHTTPClient(custom))
	if c.http != custom {
		t.Fatal("WithHTTPClient should override http.Client")
	}
}

func TestNewClient_TestEndpointOverride(t *testing.T) {
	prev := TestEndpoint
	TestEndpoint = "https://override/graphql"
	t.Cleanup(func() { TestEndpoint = prev })
	c := NewClient("k")
	if c.endpoint != "https://override/graphql" {
		t.Fatalf("endpoint = %q, want override", c.endpoint)
	}
}

func TestGetIssue_Success(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "issue(id:") {
			t.Errorf("query body missing issue: %s", body)
		}
		if got := r.Header.Get("Authorization"); got != "lin_api_test" {
			t.Errorf("Authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issue": map[string]string{
					"identifier":  "ENG-1",
					"title":       "title",
					"description": "desc",
					"url":         "https://linear.app/x",
				},
			},
		})
	})
	c := NewClient("lin_api_test", WithEndpoint(srv.URL))
	got, err := c.GetIssue(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Identifier != "ENG-1" || got.Title != "title" || got.URL != "https://linear.app/x" {
		t.Fatalf("got = %+v", got)
	}
}

func TestGetIssue_InvalidIdentifier(t *testing.T) {
	c := NewClient("k")
	_, err := c.GetIssue(context.Background(), "foo")
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Fatalf("err = %v, want ErrInvalidIdentifier", err)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issue": nil}})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.GetIssue(context.Background(), "ZZZ-9999")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "ZZZ-9999") {
		t.Fatalf("err = %q, want to mention identifier", err.Error())
	}
}

func TestGetIssue_Unauthorized(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.GetIssue(context.Background(), "ENG-1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestGetIssue_GraphQLErrors(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":   map[string]any{"issue": nil},
			"errors": []map[string]string{{"message": "bad query"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.GetIssue(context.Background(), "ENG-1")
	if err == nil || !strings.Contains(err.Error(), "bad query") {
		t.Fatalf("err = %v, want graphql 'bad query'", err)
	}
}

func TestGetIssue_NonOKStatus(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server gone"))
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.GetIssue(context.Background(), "ENG-1")
	var hErr *HTTPError
	if !errors.As(err, &hErr) || hErr.Status != http.StatusInternalServerError {
		t.Fatalf("err = %v, want *HTTPError with 500", err)
	}
}

func TestGetIssue_MalformedJSON(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not valid json"))
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.GetIssue(context.Background(), "ENG-1")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want decode error", err)
	}
}

func TestGetIssue_TransportError(t *testing.T) {
	c := NewClient("k", WithEndpoint("http://127.0.0.1:1"))
	_, err := c.GetIssue(context.Background(), "ENG-1")
	if err == nil || !strings.Contains(err.Error(), "linear: http") {
		t.Fatalf("err = %v, want transport error", err)
	}
}

func TestGetIssue_ContextCancelled(t *testing.T) {
	srv := issueServer(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GetIssue(ctx, "ENG-1")
	if err == nil {
		t.Fatal("err = nil, want context-cancellation propagation")
	}
}

func TestListProjects_Success(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"projects": map[string]any{
					"nodes": []map[string]string{
						{"id": "p1", "name": "Project One"},
						{"id": "p2", "name": "Project Two"},
					},
				},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	got, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(got) != 2 || got[0].ID != "p1" || got[1].Name != "Project Two" {
		t.Fatalf("got = %+v", got)
	}
}

func TestListProjects_GraphQLErrors(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":   map[string]any{"projects": map[string]any{"nodes": []any{}}},
			"errors": []map[string]string{{"message": "no scope"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no scope") {
		t.Fatalf("err = %v", err)
	}
}

func TestListProjects_TransportError(t *testing.T) {
	c := NewClient("k", WithEndpoint("http://127.0.0.1:1"))
	_, err := c.ListProjects(context.Background())
	if err == nil || !strings.Contains(err.Error(), "linear: http") {
		t.Fatalf("err = %v", err)
	}
}

func TestListAssignedIssues_Success(t *testing.T) {
	var seenBody string
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{
					"assignedIssues": map[string]any{
						"nodes": []map[string]any{
							{
								"identifier": "ENG-1",
								"title":      "first issue",
								"url":        "https://linear.app/eng/issue/ENG-1",
								"state":      map[string]string{"name": "In Progress"},
							},
							{
								"identifier": "ENG-2",
								"title":      "second issue",
								"url":        "https://linear.app/eng/issue/ENG-2",
								"state":      map[string]string{"name": "Todo"},
							},
						},
					},
				},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	got, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{})
	if err != nil {
		t.Fatalf("ListAssignedIssues: %v", err)
	}
	if !strings.Contains(seenBody, "viewer{assignedIssues") {
		t.Fatalf("query body missing viewer.assignedIssues: %s", seenBody)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2: %+v", len(got), got)
	}
	if got[0].Identifier != "ENG-1" || got[0].State != "In Progress" || got[0].Title != "first issue" {
		t.Fatalf("got[0] = %+v", got[0])
	}
	if got[1].Identifier != "ENG-2" || got[1].State != "Todo" {
		t.Fatalf("got[1] = %+v", got[1])
	}
}

func TestListAssignedIssues_EmptyList(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{
					"assignedIssues": map[string]any{"nodes": []any{}},
				},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	got, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{})
	if err != nil {
		t.Fatalf("ListAssignedIssues: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want empty", got)
	}
}

func TestListAssignedIssues_ProjectFilterForwarded(t *testing.T) {
	var seenBody string
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"viewer": map[string]any{
					"assignedIssues": map[string]any{"nodes": []any{}},
				},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	if _, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{ProjectID: "proj-uuid"}); err != nil {
		t.Fatalf("ListAssignedIssues: %v", err)
	}
	if !strings.Contains(seenBody, "$projectId:ID!") {
		t.Fatalf("query body declares wrong type for projectId (want ID!): %s", seenBody)
	}
	if !strings.Contains(seenBody, "project:{id:{eq:$projectId}}") {
		t.Fatalf("query body missing project filter: %s", seenBody)
	}
	if !strings.Contains(seenBody, `"projectId":"proj-uuid"`) {
		t.Fatalf("query body missing projectId variable: %s", seenBody)
	}
}

func TestListAssignedIssues_GraphQLErrors(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":   map[string]any{"viewer": map[string]any{"assignedIssues": map[string]any{"nodes": []any{}}}},
			"errors": []map[string]string{{"message": "no scope"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{})
	if err == nil || !strings.Contains(err.Error(), "no scope") {
		t.Fatalf("err = %v, want graphql 'no scope'", err)
	}
}

func TestListAssignedIssues_Unauthorized(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestListAssignedIssues_TransportError(t *testing.T) {
	c := NewClient("k", WithEndpoint("http://127.0.0.1:1"))
	_, err := c.ListAssignedIssues(context.Background(), ListIssuesOpts{})
	if err == nil || !strings.Contains(err.Error(), "linear: http") {
		t.Fatalf("err = %v", err)
	}
}
