package linear

import (
	"strings"
	"testing"
)

func TestIssueToMarkdown_WithDescription(t *testing.T) {
	got := IssueToMarkdown(Issue{
		Identifier:  "ENG-12",
		Title:       "Wire Linear",
		Description: "Body of the issue",
		URL:         "https://linear.app/eng/issue/ENG-12",
	})
	want := "# Wire Linear\n\nBody of the issue\n\n---\nLinear: https://linear.app/eng/issue/ENG-12\n"
	if got != want {
		t.Fatalf("IssueToMarkdown =\n%q\nwant\n%q", got, want)
	}
}

func TestIssueToMarkdown_EmptyDescription(t *testing.T) {
	got := IssueToMarkdown(Issue{
		Identifier:  "ENG-12",
		Title:       "Wire Linear",
		Description: "",
		URL:         "https://linear.app/eng/issue/ENG-12",
	})
	want := "# Wire Linear\n\n---\nLinear: https://linear.app/eng/issue/ENG-12\n"
	if got != want {
		t.Fatalf("IssueToMarkdown =\n%q\nwant\n%q", got, want)
	}
}

func TestIssueToMarkdown_TrimsTitleAndDescription(t *testing.T) {
	got := IssueToMarkdown(Issue{
		Title:       "  Wire Linear  ",
		Description: "  body  ",
		URL:         "https://x",
	})
	if !strings.HasPrefix(got, "# Wire Linear\n") {
		t.Fatalf("title not trimmed: %q", got)
	}
	if !strings.Contains(got, "\nbody\n") {
		t.Fatalf("description not trimmed: %q", got)
	}
}
