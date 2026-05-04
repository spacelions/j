Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
  - Seed every agent bucket:
    ```
    ./bin/j settings set planner.tool=cursor planner.model=sonnet-4 \
                       worker.tool=cursor worker.model=sonnet-4 \
                       verifier.tool=cursor verifier.model=sonnet-4
    ```
  - Stage a markdown body: `printf '# t\nbody\n' > spec.md`.

Steps:
  - Run:
    ```
    ./bin/j tasks start --from-file ./spec.md \
        --plan-requires-approval=true \
        > /tmp/start.stdout 2> /tmp/start.stderr
    echo "exit=$?"
    ```
  - Note the spawned orchestrator's PID printed in stdout and clean
    it up before listing (otherwise the reaper may flip the row out
    of `planning` mid-list):
    ```
    PID=$(awk -F'PID=' '/PID=/ { sub(/[)].*/, "", $2); print $2 }' /tmp/start.stdout)
    if [ -n "$PID" ]; then kill "$PID" 2>/dev/null; fi
    ```

Expected:
  - Exit code 0.
  - Stderr (`/tmp/start.stderr`) is empty: no warnings of any flavour
    when the bbolt write succeeds.
  - Stdout (`/tmp/start.stdout`) contains the bordered three-row
    `running in background (PID=…)` banner with a `tail -f
    .j/tasks/<id>/agent.log` line and a frame drawn with
    `┌`/`└` corners.
  - `./bin/j tasks --simple` reports a single row carrying the same
    task id printed inside the banner — the bbolt write succeeded.
