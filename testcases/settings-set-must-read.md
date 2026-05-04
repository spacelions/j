Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes --must-read=`. Confirm the `.j/` folder
    exists with `test -d .j && echo ok`. The `--must-read=` flag
    pre-seeds an empty value so the preflight check skips the
    "Files every agent must read first" prompt; the subsequent
    `settings set` calls overwrite the empty value.

Steps:
  - Run `./bin/j settings set "project.must_read=AGENTS.md;CLAUDE.md"`.
  - Run `./bin/j settings`.
  - Run `./bin/j settings set project.must_read=AGENTS.md`.
  - Run `./bin/j settings`.

Expected:
  - Each `set` invocation exits with code 0.
  - The first `j settings` listing renders, under the `[project]`
    section header, the row `  must_read = AGENTS.md;CLAUDE.md`
    exactly (case preserved on the value, semicolon-delimited,
    two-space indent; the key is rendered in kebab-case).
  - The second `j settings` listing renders, under `[project]`,
    the row `  must_read = AGENTS.md` (the previous value is
    overwritten, not appended).
