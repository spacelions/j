package testcases_test

import (
	"strings"
	"testing"
)

const allowlistCeiling = 144

func TestCoverageAllowlist_DoesNotGrow(t *testing.T) {
	body := readRepoFile(t, "coverage.allowlist")
	got := countAllowlistEntries(body)
	if got > allowlistCeiling {
		t.Fatalf(
			"coverage.allowlist has %d entries, ceiling is %d; "+
				"delete covered entries to lower the ceiling, do not raise it",
			got,
			allowlistCeiling,
		)
	}
	if got < allowlistCeiling {
		t.Fatalf(
			"good news: coverage.allowlist has %d entries; "+
				"please lower allowlistCeiling to %d",
			got,
			got,
		)
	}
}

func countAllowlistEntries(body string) int {
	var count int
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	return count
}
