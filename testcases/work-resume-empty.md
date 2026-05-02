Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j work resume`.

Expected:
  - Exit code 0.
  - Stdout contains exactly one line: `J: there are no resumable sessions`.
