package deepseek

import (
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

// envHome is the optional override DeepSeek-TUI honours for its
// session-store root; the CLI's `deepseek_home_dir()` checks this
// before falling back to `~/.deepseek`.
const envHome = "DEEPSEEK_HOME"

// sessionMeta is the subset of `~/.deepseek/sessions/<uuid>.json`
// CaptureResumeID inspects. The CLI writes a richer payload; this
// struct only decodes the metadata block so a future schema addition
// (turns, attachments, etc.) does not break us.
type sessionMeta struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}

// CaptureResumeID resolves the session id minted by the most recent
// deepseek-tui run for (workspace, since). It scans the session store
// at `$DEEPSEEK_HOME/sessions/*.json` (falling back to `~/.deepseek`)
// and returns the newest entry whose metadata.workspace matches and
// whose metadata.created_at is at or after since. A missing store
// directory yields ("", nil) — expected on a fresh machine — while a
// directory we cannot read at all surfaces a non-nil error so the
// caller can warn. Per-file decode failures are treated as misses
// rather than fatal so one corrupt file does not poison the lookup.
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

// sessionsDir resolves the directory deepseek-tui writes session JSON
// files into. DEEPSEEK_HOME wins when set; otherwise fall back to
// `~/.deepseek/sessions`. An empty home directory (no $HOME on a
// stripped environment) is reported as an error so the caller can
// warn instead of silently scanning the cwd.
func sessionsDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(envHome)); v != "" {
		return filepath.Join(v, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New(
			"deepseek: empty home directory; set DEEPSEEK_HOME",
		)
	}
	return filepath.Join(home, ".deepseek", "sessions"), nil
}

// scanSessions walks dir/*.json, decodes the metadata block, applies
// the (workspace, since) filter, and returns the matches sorted by
// CreatedAt descending so element 0 is the newest. A non-existent
// directory yields (nil, nil) so a fresh machine looks like "no
// match" rather than an error. JSON decode failures on individual
// files are skipped silently — one corrupt entry should not poison
// the scan.
func scanSessions(
	dir, workspace string, since time.Time,
) ([]sessionMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	matches := make([]sessionMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		meta, ok := decodeSession(filepath.Join(dir, entry.Name()))
		if !ok {
			continue
		}
		if meta.Workspace != workspace {
			continue
		}
		if meta.CreatedAt.Before(since) {
			continue
		}
		matches = append(matches, meta)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	return matches, nil
}

// decodeSession reads one session JSON file and returns its metadata
// block. Read or decode failures yield ok=false so scanSessions can
// skip the entry without aborting the whole scan.
func decodeSession(path string) (sessionMeta, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionMeta{}, false
	}
	var envelope struct {
		Metadata sessionMeta `json:"metadata"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return sessionMeta{}, false
	}
	if envelope.Metadata.ID == "" {
		return sessionMeta{}, false
	}
	return envelope.Metadata, true
}
