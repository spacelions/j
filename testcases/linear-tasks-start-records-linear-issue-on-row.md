Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.
  - Pre-populate every agent bucket so `EnsureAgentSelections` does not
    block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto
  - Store a Linear API key (any non-empty value works for this case,
    since the test below only asserts the row metadata after a
    successful run; it does not assert the planner output):
      ./bin/j settings set linear.api_key=lin_api_test

Steps:
  - Drive a single `--from-linear` start against a real Linear issue
    you own (replace `ENG-123` with one of your assigned identifiers):
      ./bin/j tasks start --from-linear ENG-123

Expected:
  - Exit code is 0; the bordered "running in background" banner
    prints once with the spawned PID.
  - A new task directory exists under `.j/tasks/<id>/`. Its
    `task.toml` carries the upstream identifier verbatim:
      grep '^linear_issue = "ENG-123"$' .j/tasks/*/task.toml
    matches exactly one line.
  - `./bin/j tasks` lists the new row with the same id; the
    `requirements.md` next to `task.toml` opens with the issue title
    and ends with a `Linear: <url>` footer rendered by
    `linear.IssueToMarkdown`.
  - For non-Linear sources (`--from-file <md>` or the markdown branch
    of the picker), the same `task.toml` has `linear_issue = ""` —
    the field round-trips empty when the source has no upstream
    identifier.
