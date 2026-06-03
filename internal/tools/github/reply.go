package github

import "strings"

// isReply reports whether body was authored by j. The check is
// strict on purpose: the marker alone is not enough because a
// reviewer who quotes the literal `<!-- j-code-review-reply -->`
// would otherwise have their feedback silently dropped from
// review.toml. Marker AND author-equals-viewer must both hold.
func isReply(body, author, viewer string) bool {
	if viewer == "" || author == "" {
		return false
	}
	if !strings.Contains(body, replyMarker) {
		return false
	}
	return author == viewer
}

// replyIndex memoises which feedback items already have a j reply
// attached so collectItems can populate Item.HasJReply without a
// second pass through the GraphQL response. For conversation
// comments the index is a set of source ids that immediately
// follow another non-reply comment authored by j; for review
// threads the index is a per-thread set of positions pointing at
// the comment a j reply followed. Detection is intentionally local
// to a single fetch: items added after the j reply (e.g. a later
// round's question) are not retroactively marked.
type replyIndex struct {
	conv   map[string]bool
	thread map[string]map[int]bool
}

func collectReplyAuthors(
	viewer string,
	conv []prConversationComment,
	threads []prReviewThread,
) replyIndex {
	idx := replyIndex{
		conv:   make(map[string]bool),
		thread: make(map[string]map[int]bool),
	}
	for i, c := range conv {
		if !isReply(c.Body, c.Author.Login, viewer) || i == 0 {
			continue
		}
		idx.conv[conversationSourceID(conv[i-1].ID)] = true
	}
	for _, th := range threads {
		idx.thread[th.ID] = collectThreadReplyIndex(viewer, th.Comments.Nodes)
	}
	return idx
}

func collectThreadReplyIndex(
	viewer string, comments []prReviewComment,
) map[int]bool {
	out := make(map[int]bool)
	for i, c := range comments {
		if !isReply(c.Body, c.Author.Login, viewer) || i == 0 {
			continue
		}
		out[i-1] = true
	}
	return out
}

func (r replyIndex) hasConversationReply(sourceID string) bool {
	return r.conv[sourceID]
}

func (r replyIndex) hasThreadReply(threadID string, idx int) bool {
	set := r.thread[threadID]
	if set == nil {
		return false
	}
	return set[idx]
}

// conversationSourceID, reviewCommentSourceID, and
// reviewSummarySourceID centralise the `<kind>:<node-id>`
// projection so reply.go and the item builder agree on the format.
func conversationSourceID(nodeID string) string {
	return "issue-comment:" + nodeID
}

func reviewCommentSourceID(nodeID string) string {
	return "review-comment:" + nodeID
}

func reviewSummarySourceID(nodeID string) string {
	return "review-summary:" + nodeID
}
