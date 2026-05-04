Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - Stdout renders only the four known sections (`[project]`,
    `[planner]`, `[worker]`, `[verifier]`).
  - Stdout does NOT contain a `[linear]` section: the bucket is created
    on first write only, so a fresh `j init` must not surface it.
