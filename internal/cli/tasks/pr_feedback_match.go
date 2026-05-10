package tasks

import (
	"net/url"
	"slices"
	"strings"
	"unicode"

	storetasks "github.com/spacelions/j/internal/store/tasks"
)

func tasksByPRURL(
	s *storetasks.Store,
	rawURL string,
) ([]storetasks.Task, error) {
	want := normalizePRURL(rawURL)
	rows, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	out := []storetasks.Task{}
	for _, row := range rows {
		if want != "" && normalizePRURL(row.PullRequestURL) == want {
			out = append(out, row)
		}
	}
	return out, nil
}

func normalizePRURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimRightFunc(trimmed, unicode.IsPunct)
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return trimmed
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.ToLower(strings.TrimRight(u.Path, "/"))
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func isTakeALookCommand(body string) bool {
	trimmed := strings.TrimFunc(body, commandTrimRune)
	return strings.EqualFold(strings.Join(strings.Fields(trimmed), " "),
		"@j take a look")
}

func commandTrimRune(r rune) bool {
	return unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '@')
}

func isBotUser(login string, bot bool) bool {
	return bot || strings.HasSuffix(strings.ToLower(login), "[bot]")
}

func sameLogin(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func hasProcessedCommand(task storetasks.Task, id string) bool {
	return slices.Contains(task.ProcessedPRCommands, id)
}

func taskRunning(task storetasks.Task) bool {
	switch task.Status {
	case storetasks.StatusPlanning,
		storetasks.StatusWorking,
		storetasks.StatusVerifying:
		return true
	default:
		return false
	}
}
