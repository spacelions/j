Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor`.
  - Run `./bin/j settings`.

Expected:
  - The `set` invocation exits with code 0.
  - Stdout contains the line `set planner.tool = cursor`.
  - The `j settings` listing renders the `[planner]` section with
    a single row `  tool = cursor`:

        [planner]
          tool = cursor
