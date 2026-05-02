Prerequisites:
  - Run `make` (compiles `./bin/j`).

Steps:
  - `cd` into a fresh empty directory.
  - Run `./bin/j init --yes "--must-read=AGENTS.md;CLAUDE.md"`.
  - Run `./bin/j settings`.
  - `cd` into a second fresh empty directory.
  - Run `./bin/j init --yes --must-read=`.
  - Run `./bin/j settings`.
  - `cd` into a third fresh empty directory.
  - Run `./bin/j init --yes` (no `--must-read` flag).
  - Run `./bin/j settings` from a TTY — observe whether the
    "Files every agent must read first" prompt fires.

Expected:
  - Directory 1: `j settings` renders `[project]` with the row
    `  mustread = AGENTS.md;CLAUDE.md` (case-preserved).
  - Directory 2: `j settings` renders `[project]` with the row
    `  mustread = ` (empty value persisted); `[planner]`, `[worker]`,
    and `[verifier]` render as empty section headers.
  - Directory 3: the preflight prompt fires for `j settings` because
    `--must-read` was not passed; the flag is opt-in.

Manual: yes for directory 3 (drives the huh prompt). Directory 1 and
2 are bash-runnable end-to-end.
