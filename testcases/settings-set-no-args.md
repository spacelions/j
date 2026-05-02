Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set` (no arguments). Capture the exit code.

Expected:
  - Exit code is non-zero.
  - Stderr contains cobra's standard message
    `requires at least 1 arg(s), only received 0`.
