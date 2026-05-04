Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Pre-populate every agent bucket so `EnsureAgentSelections` does not
    block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto

Steps:
  - Run `TASKS_START_FROM_LINEAR=foo ./bin/j tasks start` (no
    `--from-linear` flag, no token stored).

Expected:
  - Exit code is non-zero.
  - Output contains a single line
    `J: linear: invalid identifier (expected pattern like ENG-123): "foo"`.
    The presence of this error confirms the `TASKS_START_FROM_LINEAR`
    env-var binding is wired and routes through the same identifier
    validator as the flag.
  - No task is created: `./bin/j tasks` reports `J: no tasks`.
