package github

import (
	"context"
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
	if err := c.do(ctx, req, &resp); err != nil {
		return nil, err
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
	conv, err := fetchPaged(ctx, c, ref,
		first.Comments.PageInfo, first.Comments.Nodes,
		prCommentsPageQuery,
		func(resp *prCommentsPageResponse) (prPageInfo, []prConversationComment) {
			p := resp.Data.Repository.PullRequest.Comments
			return p.PageInfo, p.Nodes
		})
	if err != nil {
		return nil, nil, nil, err
	}
	threads, err := fetchPaged(ctx, c, ref,
		first.ReviewThreads.PageInfo, first.ReviewThreads.Nodes,
		prReviewThreadsPageQuery,
		func(resp *prReviewThreadsPageResponse) (prPageInfo, []prReviewThread) {
			p := resp.Data.Repository.PullRequest.ReviewThreads
			return p.PageInfo, p.Nodes
		})
	if err != nil {
		return nil, nil, nil, err
	}
	reviews, err := fetchPaged(ctx, c, ref,
		first.Reviews.PageInfo, first.Reviews.Nodes,
		prReviewsPageQuery,
		func(resp *prReviewsPageResponse) (prPageInfo, []prReview) {
			p := resp.Data.Repository.PullRequest.Reviews
			return p.PageInfo, p.Nodes
		})
	if err != nil {
		return nil, nil, nil, err
	}
	return conv, threads, reviews, nil
}

// fetchPaged drains a paginated PR connection. The caller passes
// the first page's pageInfo + nodes (already unmarshalled from
// the prFirstPage envelope), the per-list `*PageQuery` GraphQL
// string, and an extractor that projects each follow-up response
// onto the same `(pageInfo, nodes)` pair. The loop appends nodes
// and advances the cursor until `hasNextPage` is false.
func fetchPaged[N, R any](
	ctx context.Context,
	c *Client,
	ref PRRef,
	info prPageInfo,
	first []N,
	query string,
	extract func(*R) (prPageInfo, []N),
) ([]N, error) {
	out := first
	for info.HasNextPage {
		var resp R
		req := graphQLRequest{
			Query: query,
			Variables: map[string]any{
				"o": ref.Owner, "r": ref.Repo,
				"n": ref.Number, "a": info.EndCursor,
			},
		}
		if err := c.do(ctx, req, &resp); err != nil {
			return nil, err
		}
		nextInfo, nextNodes := extract(&resp)
		out = append(out, nextNodes...)
		info = nextInfo
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
