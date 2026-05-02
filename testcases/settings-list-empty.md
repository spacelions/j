Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings` (no subcommand) on the freshly initialised
    project.

Expected:
  - Exit code 0.
  - Stdout is exactly: `project.mustread = ` followed by a newline —
    the only row a `--mustread=` initialisation seeds. The
    "no settings stored" branch is unreachable from a cobra-driven
    init now that preflight always persists at least one row.
