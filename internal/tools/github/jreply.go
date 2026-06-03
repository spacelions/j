package github

// jReplyIndex memoises which feedback items already have a j reply
// attached so collectItems can populate Item.HasJReply without a
// second pass through the GraphQL response. For conversation comments
// the index is a set of databaseIds that immediately follow another
// non-reply comment authored by j; for review threads the index is a
// per-thread set of indexes pointing at the comment a j reply
// followed. The detection is intentionally local to a single fetch:
// items added after the j reply (e.g. a later round's question) are
// not retroactively marked.
type jReplyIndex struct {
	conv   map[int64]bool
	thread map[string]map[int]bool
}

func collectJReplyAuthors(
	conv []prConversationComment, threads []prReviewThread,
) jReplyIndex {
	idx := jReplyIndex{
		conv:   make(map[int64]bool),
		thread: make(map[string]map[int]bool),
	}
	for i, c := range conv {
		if !isJReply(c.Body) || i == 0 {
			continue
		}
		idx.conv[conv[i-1].DatabaseID] = true
	}
	for _, th := range threads {
		idx.thread[th.ID] = collectThreadJReplyIndex(th.Comments.Nodes)
	}
	return idx
}

// collectThreadJReplyIndex marks each comment position that has a j
// reply immediately after it. The boolean is keyed by the
// non-reply comment's index inside the thread so collectThreadItems
// can look it up by `i` directly.
func collectThreadJReplyIndex(comments []prReviewComment) map[int]bool {
	out := make(map[int]bool)
	for i, c := range comments {
		if !isJReply(c.Body) || i == 0 {
			continue
		}
		out[i-1] = true
	}
	return out
}

func (j jReplyIndex) hasConvReply(databaseID int64) bool {
	return j.conv[databaseID]
}

func (j jReplyIndex) hasThreadReply(threadID string, idx int) bool {
	set := j.thread[threadID]
	if set == nil {
		return false
	}
	return set[idx]
}
