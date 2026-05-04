Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Pre-populate the agent buckets so `EnsureAgentSelections` does not
    block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto

Steps:
  - Run `./bin/j tasks start --from-linear ENG-123` (no token stored).

Expected:
  - Exit code is non-zero.
  - Stderr (or combined output) contains the single line
    `J: linear: no API key set; run `j settings set linear.api_key=<lin_api_…>``
    (the `J:` prefix, the `linear: no API key set` substring, and
    the suggested `j settings set` command).
  - No task is created: `./bin/j tasks` reports `J: no tasks` and no
    background `j tasks orchestrate` child has been forked
    (no `agent.log` is written under `.j/tasks/`).
