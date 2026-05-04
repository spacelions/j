Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor`.
  - Run `./bin/j settings set planner.model=opus`.
  - Run `./bin/j settings reset planner`.
  - Run `./bin/j settings`.

Expected:
  - The `reset planner` invocation exits with code 0 and stdout
    contains exactly one `unset planner` line (no per-key `unset
    planner.tool` / `unset planner.model` lines — bucket-level reset
    emits one line per arg, not one per key).
  - The final `j settings` listing renders the four known sections in
    fixed order. The `[planner]` section appears with NO rows beneath
    it (every previously-stored key has been wiped). `[project]` still
    carries the rows that `j init` seeds plus the empty `must_read` row
    from `--must-read=`:

        [project]
          max_iterations = 3
          must_read = 
          plan_requires_approval = true

        [planner]

        [worker]

        [verifier]

  - Re-running `./bin/j settings reset planner` is a no-op success:
    exit 0 and the same `unset planner` line.
