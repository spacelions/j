Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings` (no subcommand) on the freshly initialised
    project.

Expected:
  - Exit code 0.
  - Stdout is exactly: `no settings stored`.
