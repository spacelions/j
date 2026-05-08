package linear

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestFindStateByName_Hit(t *testing.T) {
	states := []WorkflowState{
		{ID: "a", Name: "Todo", Type: "unstarted"},
		{ID: "b", Name: "In Progress", Type: "started"},
	}
	got, ok := FindStateByName(states, "In Progress")
	if !ok || got.ID != "b" {
		t.Fatalf("got = (%v, %v), want (b, true)", got, ok)
	}
}

func TestFindStateByName_Miss(t *testing.T) {
	states := []WorkflowState{
		{ID: "a", Name: "Todo", Type: "unstarted"},
	}
	got, ok := FindStateByName(states, "In Review")
	if ok || got.ID != "" {
		t.Fatalf("got = (%v, %v), want zero, false", got, ok)
	}
}

func TestListTeamWorkflowStates_OK(t *testing.T) {
	srv := issueServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"team": map[string]any{
							"states": map[string]any{
								"nodes": []map[string]string{
									{
										"id":   "x",
										"name": "Todo",
										"type": "unstarted",
									},
								},
							},
						},
					},
				},
			})
		})
	c := NewClient("k", WithEndpoint(srv.URL))
	got, err := c.ListTeamWorkflowStates(
		t.Context(), "node-1")
	if err != nil {
		t.Fatalf("ListTeamWorkflowStates: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Todo" {
		t.Fatalf("got = %+v", got)
	}
}

func TestListTeamWorkflowStates_NotFound(t *testing.T) {
	srv := issueServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"issue": nil},
			})
		})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListTeamWorkflowStates(
		t.Context(), "node-x")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListTeamWorkflowStates_GraphQLError(t *testing.T) {
	srv := issueServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":   map[string]any{"issue": nil},
				"errors": []map[string]string{{"message": "scope"}},
			})
		})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListTeamWorkflowStates(
		t.Context(), "node-x")
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err = %v", err)
	}
}

func TestListTeamWorkflowStates_Unauthorized(t *testing.T) {
	srv := issueServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	c := NewClient("k", WithEndpoint(srv.URL))
	_, err := c.ListTeamWorkflowStates(
		t.Context(), "id")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}
