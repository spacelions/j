package tasks

import "strings"

// worktreeSlugMaxRunes bounds each slug segment (project and task)
// in WorktreeNameFor. The cap keeps the resulting worktree name
// short enough to be a usable directory / branch component without
// getting in the way of `git worktree list` output.
const worktreeSlugMaxRunes = 48

// WorktreeNameFor returns the deterministic, human-readable worktree
// name for t inside project. The result is `<project-slug>-<task-slug>`,
// where each component is slugify'd (lowercase, non-[a-z0-9] runs
// collapsed to single dashes, edges trimmed, clipped to
// worktreeSlugMaxRunes runes). The task slug is derived from
// t.Summary when non-empty and falls back to the lowercased
// t.ID (a 26-char Crockford base32 ULID) so pre-summary rows still
// produce a valid name. An empty project slug yields just the task
// slug so tests that run outside a recognisable project directory
// still get a meaningful value.
//
// Examples:
//
//   - project "j", summary "Drop the legacy tasks file"
//     -> "j-drop-the-legacy-tasks-file"
//   - project "j", empty summary, id "01KQ..."
//     -> "j-01kq..."
func WorktreeNameFor(project string, t Task) string {
	projectSlug := slugify(project, worktreeSlugMaxRunes)
	taskSlug := slugify(t.Summary, worktreeSlugMaxRunes)
	if taskSlug == "" {
		// Ulid ids are always alphanumeric so slugify is effectively
		// a lowercase here; running them through slugify anyway means
		// an unexpected non-alphanumeric rune in t.ID still produces
		// a clean slug rather than leaking raw punctuation.
		taskSlug = slugify(t.ID, worktreeSlugMaxRunes)
	}
	if projectSlug == "" {
		return taskSlug
	}
	if taskSlug == "" {
		return projectSlug
	}
	return projectSlug + "-" + taskSlug
}

// slugify lowercases s, replaces every run of non-[a-z0-9] runes with
// a single `-`, trims leading/trailing `-`, and clips the result to
// max runes. An empty or pure-separator input yields "" so callers
// can fall back to a secondary id.
func slugify(s string, max int) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	return truncateRunes(out, max)
}
