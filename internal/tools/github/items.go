package github

import "strings"

// collectItems flattens conversation comments, review threads, and
// pull-request reviews into the linear feedback item slice the
// planner consumes. Order follows the GraphQL response (creation
// order) so a review.toml diff between rounds reads naturally.
//
// J's own replies are detected via marker + author match (see
// reply.go) and dropped from the items slice while still flipping
// HasJReply on whatever they reply to.
func collectItems(
	viewer string,
	conv []prConversationComment,
	threads []prReviewThread,
	reviews []prReview,
) []Item {
	items := make([]Item, 0)
	replies := collectReplyAuthors(viewer, conv, threads)
	items = appendConversationItems(items, viewer, conv, replies)
	for _, th := range threads {
		items = append(items, collectThreadItems(viewer, th, replies)...)
	}
	items = appendReviewSummaryItems(items, viewer, reviews)
	return items
}

func appendConversationItems(
	items []Item,
	viewer string,
	conv []prConversationComment,
	replies replyIndex,
) []Item {
	for _, c := range conv {
		if isReply(c.Body, c.Author.Login, viewer) {
			continue
		}
		sourceID := conversationSourceID(c.ID)
		items = append(items, Item{
			SourceID:  sourceID,
			Kind:      KindConversationComment,
			Author:    c.Author.Login,
			Body:      c.Body,
			HasJReply: replies.hasConversationReply(sourceID),
		})
	}
	return items
}

func collectThreadItems(
	viewer string, th prReviewThread, replies replyIndex,
) []Item {
	out := make([]Item, 0, len(th.Comments.Nodes))
	for i, c := range th.Comments.Nodes {
		if isReply(c.Body, c.Author.Login, viewer) {
			continue
		}
		out = append(out, Item{
			SourceID:   reviewCommentSourceID(c.ID),
			Kind:       KindReviewComment,
			ThreadID:   th.ID,
			Author:     c.Author.Login,
			Body:       c.Body,
			Path:       c.Path,
			Line:       commentLine(c),
			IsOutdated: th.IsOutdated,
			HasJReply:  replies.hasThreadReply(th.ID, i),
		})
	}
	return out
}

// commentLine picks the most useful diff line for a review
// comment. GitHub returns null `line` (which json decodes as 0)
// on outdated review threads because the comment no longer
// applies to the current diff; in that case `originalLine` carries
// the pre-rebase value. Returning the first non-zero of the two
// keeps stale-thread items pointing at a real source position
// instead of collapsing to `line: 0`.
func commentLine(c prReviewComment) int {
	if c.Line != 0 {
		return c.Line
	}
	return c.OriginalLine
}

// appendReviewSummaryItems projects PR-level review summaries with
// non-empty bodies into items. The state column (APPROVED /
// CHANGES_REQUESTED / COMMENTED) is intentionally not surfaced in
// v1 — the planner reads the body text. Empty-body reviews (a bare
// "Approve" click) are skipped so review.toml does not collect
// signal-free rows.
func appendReviewSummaryItems(
	items []Item, viewer string, reviews []prReview,
) []Item {
	for _, r := range reviews {
		body := strings.TrimSpace(r.Body)
		if body == "" {
			continue
		}
		if isReply(r.Body, r.Author.Login, viewer) {
			continue
		}
		items = append(items, Item{
			SourceID: reviewSummarySourceID(r.ID),
			Kind:     KindReviewSummary,
			Author:   r.Author.Login,
			Body:     r.Body,
		})
	}
	return items
}
