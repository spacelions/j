package github

import (
	"context"
	"fmt"
	"strings"
)

// FetchPR runs the single GraphQL round-trip that powers a code-review
// round. It returns a FetchResult carrying the PR header (URL, state,
// draft/merged flags) and the flattened feedback items the planner
// must decide on. The state guard is intentionally strict: closed,
// merged, and draft PRs each surface their own sentinel so the cli
// can render the matching dangerous dialog without re-deriving the
// reason from a wrapped string.
//
// A nil pullRequest node maps to ErrNotFound; a non-empty
// GraphQL-level `errors[]` array surfaces as a wrapped error carrying
// the first message so authentication / scope failures land with a
// useful hint.
func (c *Client) FetchPR(ctx context.Context, ref PRRef) (FetchResult, error) {
	var resp prResponse
	req := graphQLRequest{
		Query: prQuery,
		Variables: map[string]any{
			"owner":  ref.Owner,
			"repo":   ref.Repo,
			"number": ref.Number,
		},
	}
	if err := c.do(ctx, ref.Endpoint(), req, &resp); err != nil {
		return FetchResult{}, err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return FetchResult{}, fmt.Errorf("github: %s", msg)
	}
	if resp.Data.Repository == nil || resp.Data.Repository.PullRequest == nil {
		return FetchResult{}, ErrNotFound
	}
	pr := resp.Data.Repository.PullRequest
	if err := guardState(pr.State, pr.Merged, pr.IsDraft); err != nil {
		return FetchResult{}, err
	}
	return FetchResult{
		PR: PR{
			URL:    pr.URL,
			Owner:  ref.Owner,
			Repo:   ref.Repo,
			Number: ref.Number,
			State:  strings.ToLower(pr.State),
			Draft:  pr.IsDraft,
			Merged: pr.Merged,
		},
		Items: collectItems(pr.Comments.Nodes, pr.ReviewThreads.Nodes),
	}, nil
}

// guardState rejects PR states that have no review-round semantics in
// v1. MERGED wins over CLOSED because a merged PR is also reported as
// closed by GitHub; draft wins over open because a draft PR is
// reported as OPEN.
func guardState(state string, merged, isDraft bool) error {
	if merged {
		return ErrMerged
	}
	if state == "CLOSED" {
		return ErrClosed
	}
	if isDraft {
		return ErrDraft
	}
	return nil
}

// collectItems flattens the conversation comments and review-thread
// comments into the order they appear in the GraphQL response. J's
// own replies are detected via JReplyMarker so the planner can skip
// items it already answered in a previous round.
func collectItems(
	conv []prConversationComment, threads []prReviewThread,
) []Item {
	items := make([]Item, 0)
	jAuthors := collectJReplyAuthors(conv, threads)
	for _, c := range conv {
		if isJReply(c.Body) {
			continue
		}
		items = append(items, Item{
			SourceID:  fmt.Sprintf("issue-comment:%d", c.DatabaseID),
			Kind:      KindConversationComment,
			Author:    c.Author.Login,
			Body:      c.Body,
			HasJReply: jAuthors.hasConvReply(c.DatabaseID),
		})
	}
	for _, th := range threads {
		items = append(items, collectThreadItems(th, jAuthors)...)
	}
	return items
}

func collectThreadItems(th prReviewThread, jAuthors jReplyIndex) []Item {
	out := make([]Item, 0, len(th.Comments.Nodes))
	for i, c := range th.Comments.Nodes {
		if isJReply(c.Body) {
			continue
		}
		out = append(out, Item{
			SourceID:   fmt.Sprintf("review-comment:%d", c.DatabaseID),
			Kind:       KindReviewComment,
			ThreadID:   th.ID,
			Author:     c.Author.Login,
			Body:       c.Body,
			Path:       c.Path,
			Line:       c.Line,
			IsOutdated: th.IsOutdated,
			HasJReply:  jAuthors.hasThreadReply(th.ID, i),
		})
	}
	return out
}

// isJReply reports whether a comment body was authored by j (detected
// via the embedded marker comment). The marker is a stable HTML
// comment so human reviewers and the cli share a single detection
// rule.
func isJReply(body string) bool {
	return strings.Contains(body, JReplyMarker)
}
