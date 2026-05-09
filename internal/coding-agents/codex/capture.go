package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// envHome is the optional override codex honours for its session-store
// root. The CLI reads `$CODEX_HOME` before falling back to `~/.codex`;
// scripts and tests pin it the same way.
const envHome = "CODEX_HOME"

// rolloutPrefix is the filename prefix codex writes the per-session
// JSONL log under (`rollout-<isots>-<uuid>.jsonl`). The scanner uses
// it to skip unrelated files in the dated directory.
const rolloutPrefix = "rollout-"

// rolloutSuffix matches the trailing extension. We also drop empty
// or recently truncated files via os.FileInfo.Size() — see decodeMeta.
const rolloutSuffix = ".jsonl"

// sessionMeta is the subset of the first JSONL record codex writes to
// each rollout file. The full schema is much richer (model, sandbox
// policy, etc.); we only decode what CaptureResumeID needs so a
// future schema addition does not break us.
type sessionMeta struct {
	ID        string    `json:"id"`
	CWD       string    `json:"cwd"`
	Timestamp time.Time `json:"timestamp"`
}

// metaEnvelope is the wrapper line one rollout file opens with:
// {"timestamp":"...","type":"session_meta","payload":{...}}. We
// inspect type to skip non-meta first lines (defensive against a
// future schema rename).
type metaEnvelope struct {
	Type    string      `json:"type"`
	Payload sessionMeta `json:"payload"`
}

// CaptureResumeID resolves the thread id minted by the most recent
// codex run for (workspace, since). It walks
// `$CODEX_HOME/sessions/**/rollout-*.jsonl` (falling back to
// `~/.codex/sessions`) and returns the newest entry whose
// payload.cwd matches workspace and whose payload.timestamp is at or
// after since. A missing store directory yields ("", nil) — expected
// on a fresh machine — while a directory we cannot read at all
// surfaces a non-nil error so the caller can warn. Per-file decode
// failures are treated as misses rather than fatal so one corrupt
// rollout does not poison the lookup.
func (*Agent) CaptureResumeID(
	_ context.Context, workspace string, since time.Time,
) (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	metas, err := scanSessions(dir, workspace, since)
	if err != nil {
		return "", err
	}
	if len(metas) == 0 {
		return "", nil
	}
	return metas[0].ID, nil
}

// sessionsDir resolves the directory codex writes rollout files into.
// CODEX_HOME wins when set; otherwise fall back to `~/.codex/sessions`.
// An empty / unset $HOME is surfaced via os.UserHomeDir's error so the
// caller can warn instead of silently scanning the cwd.
func sessionsDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(envHome)); v != "" {
		return filepath.Join(v, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

// scanSessions walks dir recursively looking for rollout-*.jsonl
// files, decodes the first session_meta record, applies the
// (workspace, since) filter, and returns the matches sorted by
// payload.Timestamp descending so element 0 is the newest. A
// non-existent root yields (nil, nil) so a fresh machine looks like
// "no match" rather than an error. Decode failures on individual
// files are skipped silently — one corrupt rollout should not poison
// the scan.
func scanSessions(
	dir, workspace string, since time.Time,
) ([]sessionMeta, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	matches := make([]sessionMeta, 0)
	walkErr := filepath.WalkDir(dir, func(
		path string, d fs.DirEntry, err error,
	) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isRollout(d.Name()) {
			return nil
		}
		meta, ok := decodeMeta(path)
		if !ok {
			return nil
		}
		if meta.CWD != workspace || meta.Timestamp.Before(since) {
			return nil
		}
		matches = append(matches, meta)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})
	return matches, nil
}

// isRollout pins the filename shape codex uses for per-session JSONL
// logs (`rollout-<isots>-<uuid>.jsonl`). Anything else in the dated
// directory tree is skipped.
func isRollout(name string) bool {
	return strings.HasPrefix(name, rolloutPrefix) &&
		strings.HasSuffix(name, rolloutSuffix)
}

// decodeMeta opens path, reads only the first line (the rollout's
// `session_meta` envelope), and returns its payload. Any read or
// decode failure yields ok=false so the scanner skips the entry.
func decodeMeta(path string) (sessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return sessionMeta{}, false
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReader(f)
	line, err := br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return sessionMeta{}, false
	}
	var env metaEnvelope
	if jerr := json.Unmarshal(line, &env); jerr != nil {
		return sessionMeta{}, false
	}
	if env.Type != "session_meta" || env.Payload.ID == "" {
		return sessionMeta{}, false
	}
	return env.Payload, true
}
