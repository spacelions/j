package codingagents

// PRFeedbackContext is the structured pull-request discussion
// context passed to the planner for PR-feedback triage.
type PRFeedbackContext struct {
	PullRequestURL        string
	PullRequestTitle      string
	PullRequestAuthor     string
	InvocationCommentID   string
	InvocationCommentBody string
	Comments              []PRFeedbackComment
}

// PRFeedbackComment is one PR discussion item included as untrusted
// review feedback in a PR-feedback planner run.
type PRFeedbackComment struct {
	ID       string
	Author   string
	Body     string
	URL      string
	Resolved bool
}
