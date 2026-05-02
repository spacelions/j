Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes --mustread=`. Confirm the `.j/` folder
    exists with `test -d .j && echo ok`. The `--mustread=` flag
    pre-seeds an empty value so the preflight check skips the
    "Files every agent must read first" prompt; the subsequent
    `settings set` calls overwrite the empty value.

Steps:
  - Run `./bin/j settings set "project.mustread=AGENTS.md;CLAUDE.md"`.
  - Run `./bin/j settings`.
  - Run `./bin/j settings set project.mustread=AGENTS.md`.
  - Run `./bin/j settings`.

Expected:
  - Each `set` invocation exits with code 0.
  - The first `j settings` listing contains the row
    `project.mustread = AGENTS.md;CLAUDE.md` exactly (case preserved,
    semicolon-delimited).
  - The second `j settings` listing contains
    `project.mustread = AGENTS.md` (the previous value is overwritten,
    not appended).
