Prerequisites:
  - From the worktree root, run `make build` to compile `./bin/j`.
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
  - Pre-populate the agent buckets so `EnsureAgentSelections` does not
    block on a TTY prompt:
      ./bin/j settings set planner.tool=cursor planner.model=auto \
                            worker.tool=cursor worker.model=auto \
                            verifier.tool=cursor verifier.model=auto
  - Confirm no Linear API key is stored:
      ./bin/j settings reset linear.api_key 2>/dev/null || true

Steps:
  - Run `./bin/j tasks start --from-linear ENG-123`.
  - Capture the exit code (`echo "exit=$?"`) and the combined output.

Expected:
  - Exit code is 1 (non-zero).
  - The combined output contains the verbatim PR #83 wording:
      `J: linear: no API key set; run `j settings set linear.api_key=<lin_api_…>``
  - No task directory is created under `.j/tasks/`.
  - No `agent.log` is written and no `j tasks orchestrate` child is
    spawned. `./bin/j tasks` reports `J: no tasks`.
