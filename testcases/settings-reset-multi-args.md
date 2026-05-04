Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor planner.model=opus worker.model=sonnet verifier.tool=cursor`.
  - Run `./bin/j settings reset planner worker.model verifier`.
  - Run `./bin/j settings`.

Expected:
  - The `reset` invocation exits with code 0 and stdout contains the
    following three lines, in this exact order (left-to-right by arg
    position; one line per positional target — bucket-level targets
    do not fan out per key):

        unset planner
        unset worker.model
        unset verifier

  - The final `j settings` listing has empty `[planner]`, `[worker]`
    and `[verifier]` sections (every previously-stored key is gone).
    `[project]` still carries the rows that `j init` seeds plus the
    empty `must_read` row from `--must-read=`:

        [project]
          max_iterations = 3
          must_read = 
          plan_requires_approval = true

        [planner]

        [worker]

        [verifier]

  - Whitespace is the only separator. `./bin/j settings reset
    planner,worker.model` does NOT split on the comma — the literal
    target string `planner,worker.model` is parsed as `bucket=planner,worker`
    with `key=model`, which is a no-op (no such bucket exists) and
    emits `unset planner,worker.model`. Use spaces between targets.
