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
  - Corrupt the bbolt task DB so `bolt.Open` fails with a
    non-timeout error (the file still exists, so preflight's
    ProjectInitialized check passes; bolt rejects the magic-number
    mismatch with `invalid database`):
    ```
    printf 'garbage data not bbolt format\n' > .j/tasks/list.db
    ```

Steps:
  - Run:
    ```
    ./bin/j tasks start --from-file ./spec.md \
        --plan-requires-approval=true \
        > /tmp/start.stdout 2> /tmp/start.stderr
    echo "exit=$?"
    ```
  - Note the spawned orchestrator's PID printed in stdout and clean
    it up:
    ```
    PID=$(awk -F'PID=' '/PID=/ { sub(/[)].*/, "", $2); print $2 }' /tmp/start.stdout)
    if [ -n "$PID" ]; then kill "$PID" 2>/dev/null; fi
    ```

Expected:
  - Exit code 0 (RunStart only suppresses the banner on
    `ErrOpenTimeout`; non-timeout failures keep both the legacy
    wording and the banner).
  - Stderr (`/tmp/start.stderr`) contains a line beginning with
    `J: warning: tasks db: store: open` and ending with `invalid
    database` (the legacy wording) and does NOT contain
    `■ J: cannot write to database`.
  - Stdout (`/tmp/start.stdout`) DOES contain the bordered
    `running in background` banner — non-timeout failures retain
    the existing surface so debuggable failures stay visible.
