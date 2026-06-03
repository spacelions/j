// Package codereview models the per-round review.toml file used by
// `j tasks code-review`. The fetched-feedback portion is written
// before the planner runs; the planner appends decisions and the
// validator re-reads the file before the round is treated as
// complete. The model and validator are kept inside the cli/tasks
// tree (rather than internal/tools/github) so the github package
// stays a pure GraphQL client and the validator can depend on the
// store/tasks helpers without an import cycle.
package codereview

import (
	"time"
)

// ReviewFile is the on-disk shape of `review.toml`. SchemaVersion
// pins the layout so future migrations can branch without breaking
// historical rounds.
type ReviewFile struct {
	SchemaVersion int       `toml:"schema_version"`
	Provider      string    `toml:"provider"`
	FetchedAt     time.Time `toml:"fetched_at"`
	Decision      string    `toml:"decision,omitempty"`
	Summary       string    `toml:"summary,omitempty"`
	PR            PR        `toml:"pr"`
	Items         []Item    `toml:"items"`
}

// PR is the `[pr]` block. Mirrors the github.PR struct fields but
// stays in this package so the planner-facing TOML shape never leaks
// the GraphQL response type.
type PR struct {
	URL    string `toml:"url"`
	Owner  string `toml:"owner"`
	Repo   string `toml:"repo"`
	Number int    `toml:"number"`
	State  string `toml:"state"`
	Draft  bool   `toml:"draft"`
	Merged bool   `toml:"merged"`
}

// Item is one `[[items]]` block. Every field documented in plan.md
// is represented; planner-only fields (decision, reason, reply,
// plan_ref) are omitempty so the fetched-only round serialises
// without empty strings cluttering the file.
type Item struct {
	SourceID   string `toml:"source_id"`
	Kind       string `toml:"kind"`
	ThreadID   string `toml:"thread_id,omitempty"`
	Author     string `toml:"author"`
	Body       string `toml:"body"`
	Path       string `toml:"path,omitempty"`
	Line       int    `toml:"line,omitempty"`
	IsOutdated bool   `toml:"is_outdated"`
	HasJReply  bool   `toml:"has_j_reply"`
	Decision   string `toml:"decision,omitempty"`
	Reason     string `toml:"reason,omitempty"`
	Reply      string `toml:"reply,omitempty"`
	PlanRef    string `toml:"plan_ref,omitempty"`
}

// AllowedItemDecisions enumerates the per-item decisions the planner
// may set. Validation rejects anything else so a typo or invented
// state never reaches future posting code.
var AllowedItemDecisions = map[string]bool{
	"":               true, // not yet decided is allowed pre-planner
	"accepted":       true,
	"rejected":       true,
	"clarification":  true,
	"non_actionable": true,
}

// AllowedTopDecisions enumerates the top-level `decision` values the
// planner may set. Empty is allowed pre-planner; the validator
// requires non-empty when called as ValidatePost.
var AllowedTopDecisions = map[string]bool{
	"":                     true,
	"changes_needed":       true,
	"no_changes_needed":    true,
	"clarification_needed": true,
}

// ReplyMaxRunes caps the planner's draft reply length so the future
// posting code does not need to truncate. 280 chars matches the
// prompt contract.
const ReplyMaxRunes = 280
