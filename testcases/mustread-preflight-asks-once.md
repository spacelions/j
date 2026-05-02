Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`.
    Do NOT pass `--mustread` — this test exercises the preflight
    prompt path. Confirm `.j/` exists with `test -d .j && echo ok`.

Steps (MANUAL — requires a TTY):
  - Run `./bin/j tasks` (or any preflight-gated subcommand other
    than `init`). The new huh form titled
    "Files every agent must read first" should appear with a
    placeholder showing `AGENTS.md;CLAUDE.md`.
  - Type `AGENTS.md;CLAUDE.md` and press enter.
  - Run `./bin/j tasks` again.
  - Run `./bin/j settings`.

Expected:
  - The first `j tasks` invocation prompts for must-read once.
  - The second `j tasks` invocation does NOT re-prompt — the
    persisted value short-circuits the preflight check.
  - The `j settings` listing contains
    `project.mustread = AGENTS.md;CLAUDE.md`.

Manual: yes (drives the huh input form).
