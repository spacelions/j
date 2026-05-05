package resolver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spacelions/j/internal/store/tasks"
)

func ReadVerdictForTask(taskID string) string {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return "FAIL"
	}
	return ParseVerdict(filepath.Join(tasksDir, taskID, tasks.VerifierFindingsFileName))
}

var verdictRegexp = regexp.MustCompile(`(?i)^\s*VERDICT:\s*(PASS|FAIL)\s*$`)

func ParseVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "FAIL"
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := verdictRegexp.FindStringSubmatch(line)
		if m == nil {
			return "FAIL"
		}
		return strings.ToUpper(m[1])
	}
	return "FAIL"
}
