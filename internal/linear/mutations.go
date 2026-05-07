package linear

import (
	"context"
	"fmt"
)

// issueUpdateMutation overwrites the description of the issue
// addressed by node id. The `success` field is the only thing the
// caller needs — Linear returns false if the input failed
// validation but the request itself was well-formed.
const issueUpdateMutation = `mutation($id:String!,$body:String!){` +
	`issueUpdate(id:$id,input:{description:$body}){success}}`

// commentCreateMutation posts a new comment on the issue addressed
// by node id. Linear scopes comments per call so a re-plan adds a
// fresh comment rather than editing the previous one.
const commentCreateMutation = `mutation($id:String!,$body:String!){` +
	`commentCreate(input:{issueId:$id,body:$body}){success}}`

// issueUpdateStateMutation moves the issue addressed by node id to a
// different workflow state. Linear's input shape is the same
// `issueUpdate` mutation as the description-update path; the only
// field set is `stateId`.
const issueUpdateStateMutation = `mutation($id:String!,$stateId:String!){` +
	`issueUpdate(id:$id,input:{stateId:$stateId}){success}}`

// mutationResponse captures only the part of a mutation response
// the client cares about — Linear's `success` field is informational
// (the GraphQL endpoint already surfaces failures via `errors[]`).
type mutationResponse struct {
	Errors []graphQLError `json:"errors"`
}

// UpdateIssueDescription overwrites the description of the issue
// addressed by issueID (the GraphQL node id, not the `<TEAM>-<NUM>`
// identifier). Used by the linear-push hook to mirror the planner's
// refined `requirements.md` back to the upstream issue. GraphQL
// errors are wrapped as `linear: <msg>`; 401 maps to ErrUnauthorized
// and other non-2xx statuses surface as *HTTPError, mirroring the
// query-side helpers.
func (c *Client) UpdateIssueDescription(
	ctx context.Context, issueID, body string,
) error {
	return c.runMutation(ctx, issueUpdateMutation, issueID, body)
}

// CreateComment posts body as a new comment on the issue addressed
// by issueID (GraphQL node id). Used by the linear-push hook to
// mirror `plan.md` back to Linear after a successful plan
// transition. Each call posts a *new* comment — re-planning the
// same task therefore appends rather than edits. Error mapping
// matches UpdateIssueDescription.
func (c *Client) CreateComment(
	ctx context.Context, issueID, body string,
) error {
	return c.runMutation(ctx, commentCreateMutation, issueID, body)
}

// UpdateIssueState moves the issue addressed by issueID to the
// workflow state addressed by stateID (both GraphQL node ids). Used
// by the linear-state-sync hook to mirror J's lifecycle into Linear.
// Error mapping matches UpdateIssueDescription.
func (c *Client) UpdateIssueState(
	ctx context.Context, issueID, stateID string,
) error {
	var resp mutationResponse
	req := graphQLRequest{
		Query: issueUpdateStateMutation,
		Variables: map[string]any{
			"id": issueID, "stateId": stateID,
		},
	}
	if err := c.do(ctx, req, &resp); err != nil {
		return err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return fmt.Errorf("linear: %s", msg)
	}
	return nil
}

// CreateMentionComment posts a new comment whose body is prefixed
// with a `@<viewerID>` mention pill — Linear renders the bare UUID
// as a user mention pill in the issue thread. Used by the
// linear-state-sync hook so the API-key owner is notified when a
// task transitions into a state that needs human attention.
func (c *Client) CreateMentionComment(
	ctx context.Context, issueID, viewerID, body string,
) error {
	return c.runMutation(
		ctx, commentCreateMutation, issueID,
		"@"+viewerID+" "+body,
	)
}

// runMutation is the shared transport for the (id, body) mutations.
// Both issueUpdate and commentCreate take the same variable shape so
// the call sites collapse to a single helper that builds the request,
// dispatches via c.do, and converts a `errors[]` response into a
// wrapped `linear: <msg>` error.
func (c *Client) runMutation(
	ctx context.Context, query, issueID, body string,
) error {
	var resp mutationResponse
	req := graphQLRequest{
		Query:     query,
		Variables: map[string]any{"id": issueID, "body": body},
	}
	if err := c.do(ctx, req, &resp); err != nil {
		return err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return fmt.Errorf("linear: %s", msg)
	}
	return nil
}
