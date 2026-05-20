package resolver

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spacelions/j/internal/store/tasks"
)

// VerdictPass and VerdictFail are the two canonical verdict strings
// written by the verifier agent and parsed back by ParseVerdict.
const (
	VerdictPass = "PASS"
	VerdictFail = "FAIL"
)

func ReadVerdictForTask(taskID string) string {
	// A failed DefaultDir flows through as an empty tasks root; the
	// resulting path is unreadable and ParseVerdict's ReadFile error
	// branch returns VerdictFail — identical observed behaviour to an
	// explicit early return.
	tasksDir, _ := tasks.DefaultDir()
	return ParseVerdict(filepath.Join(
		tasksDir, taskID, tasks.VerifierFindingsFileName))
}

var verdictRegexp = regexp.MustCompile(`(?i)^\s*VERDICT:\s*(PASS|FAIL)\s*$`)

func ParseVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return VerdictFail
	}
	lines := strings.Split(string(data), "\n")
	for _, v := range slices.Backward(lines) {
		line := strings.TrimRight(v, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := verdictRegexp.FindStringSubmatch(line)
		if m == nil {
			return VerdictFail
		}
		return strings.ToUpper(m[1])
	}
	return VerdictFail
}
