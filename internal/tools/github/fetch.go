package github

import (
	"context"
	"fmt"
	"strings"
)

// FetchPR runs the GraphQL conversation needed for a code-review
// round. It paginates conversation comments, review threads, and
// pull-request reviews until every connection is drained, then
// flattens the result into FetchResult. The state guard is strict:
// closed, merged, and draft PRs each surface their own sentinel so
// the cli can render the matching dangerous output without re-
// deriving the reason from a wrapped string.
func (c *Client) FetchPR(ctx context.Context, ref PRRef) (FetchResult, error) {
	viewer, err := c.Viewer(ctx)
	if err != nil {
		return FetchResult{}, err
	}
	first, err := c.fetchPRFirstPage(ctx, ref)
	if err != nil {
		return FetchResult{}, err
	}
	if err := guardState(first.State, first.Merged, first.IsDraft); err != nil {
		return FetchResult{}, err
	}
	conv, threads, reviews, err := c.fetchRemaining(ctx, ref, first)
	if err != nil {
		return FetchResult{}, err
	}
	return FetchResult{
		PR: PR{
			URL:    first.URL,
			Owner:  ref.Owner,
			Repo:   ref.Repo,
			Number: ref.Number,
			State:  strings.ToLower(first.State),
			Draft:  first.IsDraft,
			Merged: first.Merged,
		},
		Items: collectItems(viewer, conv, threads, reviews),
	}, nil
}

func (c *Client) fetchPRFirstPage(
	ctx context.Context, ref PRRef,
) (*prFirstPage, error) {
	var resp prFirstPageResponse
	req := graphQLRequest{
		Query: prFirstPageQuery,
		Variables: map[string]any{
			"o": ref.Owner, "r": ref.Repo, "n": ref.Number,
		},
	}
	if err := c.do(ctx, ref.Endpoint(), req, &resp); err != nil {
		return nil, err
	}
	if msg := firstGraphQLError(resp.Errors); msg != "" {
		return nil, fmt.Errorf("github: %s", msg)
	}
	if resp.Data.Repository == nil || resp.Data.Repository.PullRequest == nil {
		return nil, ErrNotFound
	}
	return resp.Data.Repository.PullRequest, nil
}

// fetchRemaining drains every connection past the first page. Each
// list is paginated independently; the rare >100-comment review
// thread keeps its first page only (documented limit, see
// prReviewThreadsPageQuery).
func (c *Client) fetchRemaining(
	ctx context.Context, ref PRRef, first *prFirstPage,
) ([]prConversationComment, []prReviewThread, []prReview, error) {
	conv, err := c.fetchAllComments(ctx, ref, first.Comments)
	if err != nil {
		return nil, nil, nil, err
	}
	threads, err := c.fetchAllReviewThreads(ctx, ref, first.ReviewThreads)
	if err != nil {
		return nil, nil, nil, err
	}
	reviews, err := c.fetchAllReviews(ctx, ref, first.Reviews)
	if err != nil {
		return nil, nil, nil, err
	}
	return conv, threads, reviews, nil
}

func (c *Client) fetchAllComments(
	ctx context.Context, ref PRRef, page prConversationCommentsPage,
) ([]prConversationComment, error) {
	out := page.Nodes
	info := page.PageInfo
	for info.HasNextPage {
		var resp prCommentsPageResponse
		req := graphQLRequest{
			Query: prCommentsPageQuery,
			Variables: map[string]any{
				"o": ref.Owner, "r": ref.Repo,
				"n": ref.Number, "a": info.EndCursor,
			},
		}
		if err := c.do(ctx, ref.Endpoint(), req, &resp); err != nil {
			return nil, err
		}
		if msg := firstGraphQLError(resp.Errors); msg != "" {
			return nil, fmt.Errorf("github: %s", msg)
		}
		next := resp.Data.Repository.PullRequest.Comments
		out = append(out, next.Nodes...)
		info = next.PageInfo
	}
	return out, nil
}

func (c *Client) fetchAllReviewThreads(
	ctx context.Context, ref PRRef, page prReviewThreadsPage,
) ([]prReviewThread, error) {
	out := page.Nodes
	info := page.PageInfo
	for info.HasNextPage {
		var resp prReviewThreadsPageResponse
		req := graphQLRequest{
			Query: prReviewThreadsPageQuery,
			Variables: map[string]any{
				"o": ref.Owner, "r": ref.Repo,
				"n": ref.Number, "a": info.EndCursor,
			},
		}
		if err := c.do(ctx, ref.Endpoint(), req, &resp); err != nil {
			return nil, err
		}
		if msg := firstGraphQLError(resp.Errors); msg != "" {
			return nil, fmt.Errorf("github: %s", msg)
		}
		next := resp.Data.Repository.PullRequest.ReviewThreads
		out = append(out, next.Nodes...)
		info = next.PageInfo
	}
	return out, nil
}

func (c *Client) fetchAllReviews(
	ctx context.Context, ref PRRef, page prReviewsPage,
) ([]prReview, error) {
	out := page.Nodes
	info := page.PageInfo
	for info.HasNextPage {
		var resp prReviewsPageResponse
		req := graphQLRequest{
			Query: prReviewsPageQuery,
			Variables: map[string]any{
				"o": ref.Owner, "r": ref.Repo,
				"n": ref.Number, "a": info.EndCursor,
			},
		}
		if err := c.do(ctx, ref.Endpoint(), req, &resp); err != nil {
			return nil, err
		}
		if msg := firstGraphQLError(resp.Errors); msg != "" {
			return nil, fmt.Errorf("github: %s", msg)
		}
		next := resp.Data.Repository.PullRequest.Reviews
		out = append(out, next.Nodes...)
		info = next.PageInfo
	}
	return out, nil
}

// guardState rejects PR states that have no review-round semantics
// in v1. MERGED wins over CLOSED because a merged PR is also
// reported as closed by GitHub; draft wins over open because a
// draft PR is reported as OPEN.
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
