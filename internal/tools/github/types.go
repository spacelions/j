package github

// PR is the per-PR header returned by FetchPR. It carries the metadata
// the code-review command needs to (a) reject closed/merged/draft PRs
// before any planner work and (b) populate the `[pr]` block in the
// round's review.toml. Fields use the upstream wording (state is the
// GraphQL `PullRequestState` enum: OPEN / CLOSED / MERGED, lowercased
// on the way out).
type PR struct {
	URL    string
	Owner  string
	Repo   string
	Number int
	State  string
	Draft  bool
	Merged bool
}

// ItemKind tags an Item with the upstream feedback source. The string
// values land verbatim in the round's review.toml so a single
// FetchPR call yields review.toml-ready rows without a separate
// translation step.
type ItemKind string

const (
	// KindConversationComment is a top-level issue comment posted
	// against the PR conversation tab.
	KindConversationComment ItemKind = "conversation_comment"
	// KindReviewComment is an inline review comment posted against a
	// specific file/line as part of a review thread.
	KindReviewComment ItemKind = "review_comment"
	// KindReviewSummary is the body of a pull-request review
	// (Approve / Request changes / Comment) when the reviewer left
	// text in the summary box rather than only inline. These items
	// have no file path or line; their Body and Author still drive
	// the planner.
	KindReviewSummary ItemKind = "review_summary"
)

// Item is a single feedback row the planner must decide on. SourceID
// is the stable identifier the planner-updated review.toml must echo
// back verbatim; the code-review validator rejects rounds whose
// updated TOML drops or invents source ids. ThreadID is populated for
// review_comment kinds so the future posting code can post replies on
// the right thread without re-fetching. HasJReply lets the planner
// skip items J already responded to in earlier rounds.
type Item struct {
	SourceID   string
	Kind       ItemKind
	ThreadID   string
	Author     string
	Body       string
	Path       string
	Line       int
	IsOutdated bool
	HasJReply  bool
}

// FetchResult is the aggregate FetchPR returns. The slice ordering
// follows the GraphQL response (creation order from the server) so a
// review.toml diff between rounds reads naturally.
type FetchResult struct {
	PR    PR
	Items []Item
}
