Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Pre-populate the agent buckets and the Linear API key so the
    invalid-identifier branch is the only failure path exercised:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto \
                            linear.api_key=lin_api_TESTTOKEN

Steps:
  - Run `./bin/j tasks start --from-linear foo`.

Expected:
  - Exit code is non-zero.
  - Output contains a single line
    `J: linear: invalid identifier (expected pattern like ENG-123): "foo"`.
  - No task is created: `./bin/j tasks` reports `J: no tasks`.
