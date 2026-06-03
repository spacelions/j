package testcases_test

import (
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store/codereview"
)

// TestCodeReviewRequiresItemDecisions verifies the review artifact
// acceptance rule that planner-updated review.toml must carry a
// concrete decision for every fetched review item.
func TestCodeReviewRequiresItemDecisions(t *testing.T) {
	file := codereview.ReviewFile{
		SchemaVersion: codereview.SchemaVersion,
		Provider:      "github",
		FetchedAt:     time.Now().UTC(),
		Decision:      "changes_needed",
		PR: codereview.PR{
			URL:    "https://github.com/acme/app/pull/42",
			Owner:  "acme",
			Repo:   "app",
			Number: 42,
			State:  "open",
		},
		Items: []codereview.Item{{
			SourceID: "review-comment:456",
			Kind:     "review_comment",
			Author:   "alice",
			Body:     "This can panic when input is nil.",
		}},
	}

	err := codereview.ValidatePost(
		file, codereview.SnapshotSourceIDs(file))
	if err == nil {
		t.Fatal("review.toml with an undecided item passed validation")
	}
	if !strings.Contains(err.Error(), "missing decision") {
		t.Fatalf("err = %v, want missing decision", err)
	}
}
