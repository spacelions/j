Prerequisites:
  - From the worktree root (`j-rebase-pr-83-linear-as-source-onto-the-new-toml-`),
    run `make build` to compile `./bin/j`.
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm `.j/` is created.
  - Pre-populate every agent bucket so `EnsureAgentSelections` doesn't
    block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto

Steps:
  - Stage a tiny markdown task description:
      printf '# Sample\nA short markdown task description.\n' > req.md
  - Drive a single non-Linear start:
      ./bin/j tasks start --from-file req.md
  - Stop the spawned background child immediately
    (`pkill -f 'tasks orchestrate'` is fine for the test).

Expected:
  - Exit code is 0; the bordered "running in background" banner prints
    once with the spawned PID.
  - A single new task directory exists under `.j/tasks/<id>/` whose
    `task.toml` ends with the literal line `linear_issue = ''`.
    Equivalently, `grep '^linear_issue' .j/tasks/*/task.toml` matches
    exactly one line and the value is the empty string.
  - The TOML round-trips cleanly: `./bin/j tasks` lists the new row
    with no error.
