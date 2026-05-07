package linear

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// decodeReq decodes a recorded request body into a graphQLRequest.
// Used by mutation tests to assert the variables the client sent.
func decodeReq(t *testing.T, body []byte) graphQLRequest {
	t.Helper()
	var req graphQLRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode req: %v (body=%s)", err, body)
	}
	return req
}

func TestUpdateIssueDescription_OK(t *testing.T) {
	var seenBody []byte
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]any{"success": true},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueDescription(
		context.Background(), "node-id-1", "new body")
	if err != nil {
		t.Fatalf("UpdateIssueDescription: %v", err)
	}
	if !strings.Contains(string(seenBody), "issueUpdate") {
		t.Fatalf("body missing issueUpdate: %s", seenBody)
	}
	req := decodeReq(t, seenBody)
	if req.Variables["id"] != "node-id-1" {
		t.Fatalf("id var = %v", req.Variables["id"])
	}
	if req.Variables["body"] != "new body" {
		t.Fatalf("body var = %v", req.Variables["body"])
	}
}

func TestUpdateIssueDescription_GraphQLError(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]any{"success": false},
			},
			"errors": []map[string]string{{"message": "bad input"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueDescription(context.Background(), "id", "b")
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("err = %v, want graphql 'bad input'", err)
	}
}

func TestUpdateIssueDescription_Unauthorized(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueDescription(context.Background(), "id", "b")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestUpdateIssueDescription_HTTP500(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueDescription(context.Background(), "id", "b")
	var hErr *HTTPError
	if !errors.As(err, &hErr) || hErr.Status != http.StatusInternalServerError {
		t.Fatalf("err = %v, want *HTTPError with 500", err)
	}
}

func TestCreateComment_OK(t *testing.T) {
	var seenBody []byte
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{"success": true},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateComment(
		context.Background(), "node-id-2", "hello plan")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if !strings.Contains(string(seenBody), "commentCreate") {
		t.Fatalf("body missing commentCreate: %s", seenBody)
	}
	req := decodeReq(t, seenBody)
	if req.Variables["id"] != "node-id-2" {
		t.Fatalf("id var = %v", req.Variables["id"])
	}
	if req.Variables["body"] != "hello plan" {
		t.Fatalf("body var = %v", req.Variables["body"])
	}
}

func TestCreateComment_GraphQLError(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{"success": false},
			},
			"errors": []map[string]string{{"message": "no perms"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateComment(context.Background(), "id", "b")
	if err == nil || !strings.Contains(err.Error(), "no perms") {
		t.Fatalf("err = %v, want graphql 'no perms'", err)
	}
}

func TestCreateComment_Unauthorized(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateComment(context.Background(), "id", "b")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestCreateComment_HTTP500(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("dead"))
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateComment(context.Background(), "id", "b")
	var hErr *HTTPError
	if !errors.As(err, &hErr) || hErr.Status != http.StatusInternalServerError {
		t.Fatalf("err = %v, want *HTTPError with 500", err)
	}
}

func TestUpdateIssueState_OK(t *testing.T) {
	var seenBody []byte
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]any{"success": true},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueState(
		context.Background(), "node-1", "state-1")
	if err != nil {
		t.Fatalf("UpdateIssueState: %v", err)
	}
	if !strings.Contains(string(seenBody), "stateId") {
		t.Fatalf("body missing stateId: %s", seenBody)
	}
	req := decodeReq(t, seenBody)
	if req.Variables["id"] != "node-1" {
		t.Fatalf("id var = %v", req.Variables["id"])
	}
	if req.Variables["stateId"] != "state-1" {
		t.Fatalf("stateId var = %v", req.Variables["stateId"])
	}
}

func TestUpdateIssueState_GraphQLError(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]any{"success": false},
			},
			"errors": []map[string]string{{"message": "bad state"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueState(context.Background(), "id", "s")
	if err == nil || !strings.Contains(err.Error(), "bad state") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateIssueState_Unauthorized(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.UpdateIssueState(context.Background(), "id", "s")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestCreateMentionComment_PrependsViewerMention(t *testing.T) {
	var seenBody []byte
	srv := issueServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{"success": true},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateMentionComment(
		context.Background(), "id-1", "viewer-uuid", "todo")
	if err != nil {
		t.Fatalf("CreateMentionComment: %v", err)
	}
	req := decodeReq(t, seenBody)
	if req.Variables["body"] != "@viewer-uuid todo" {
		t.Fatalf("body = %v, want '@viewer-uuid todo'",
			req.Variables["body"])
	}
}

func TestCreateMentionComment_GraphQLError(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{"success": false},
			},
			"errors": []map[string]string{{"message": "x"}},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	err := c.CreateMentionComment(
		context.Background(), "id", "v", "b")
	if err == nil || !strings.Contains(err.Error(), "x") {
		t.Fatalf("err = %v", err)
	}
}

// TestGetIssue_PopulatesID confirms the GraphQL `id` field round-
// trips into Issue.ID. The mutations need it as the address argument
// so a regression here would silently break the linear-push hook.
func TestGetIssue_PopulatesID(t *testing.T) {
	srv := issueServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issue": map[string]string{
					"id":          "node-abc",
					"identifier":  "ENG-1",
					"title":       "t",
					"description": "d",
					"url":         "u",
				},
			},
		})
	})
	c := NewClient("k", WithEndpoint(srv.URL))
	got, err := c.GetIssue(context.Background(), "ENG-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.ID != "node-abc" {
		t.Fatalf("ID = %q, want node-abc", got.ID)
	}
}
