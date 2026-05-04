Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - Build a tiny lock-holder helper that opens the bbolt task DB and
    holds the file lock until killed. The helper lives outside the
    repo to avoid a test-only dependency:
    ```
    mkdir -p /tmp/verify-bbolt-lock/lh
    cat > /tmp/verify-bbolt-lock/lh/main.go <<'EOF'
    package main

    import (
        "flag"
        "fmt"
        "os"
        "os/signal"
        "syscall"
        "time"

        bolt "go.etcd.io/bbolt"
    )

    func main() {
        path := flag.String("path", "", "path to bbolt file")
        hold := flag.Duration("hold", 30*time.Second, "how long to hold")
        ready := flag.String("ready", "", "touch this file once locked")
        flag.Parse()
        if *path == "" { fmt.Fprintln(os.Stderr, "lockholder: -path required"); os.Exit(2) }
        db, err := bolt.Open(*path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
        if err != nil { fmt.Fprintln(os.Stderr, "lockholder: open:", err); os.Exit(1) }
        defer db.Close()
        if *ready != "" { _ = os.WriteFile(*ready, []byte("ok\n"), 0o600) }
        sigs := make(chan os.Signal, 1)
        signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
        select {
        case <-sigs:
        case <-time.After(*hold):
        }
    }
    EOF
    cat > /tmp/verify-bbolt-lock/go.mod <<'EOF'
    module lockholder

    go 1.24

    require go.etcd.io/bbolt v1.4.3
    EOF
    (cd /tmp/verify-bbolt-lock && go mod tidy && go build -o lockholder ./lh)
    ```
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
  - Seed every agent bucket so the start command does not prompt:
    ```
    ./bin/j settings set planner.tool=cursor planner.model=sonnet-4 \
                       worker.tool=cursor worker.model=sonnet-4 \
                       verifier.tool=cursor verifier.model=sonnet-4
    ```
  - Stage a markdown body: `printf '# t\nbody\n' > spec.md`.

Steps:
  - In a sub-shell, hold the bbolt lock in the background:
    ```
    rm -f /tmp/lock.ready
    /tmp/verify-bbolt-lock/lockholder \
        -path "$(pwd)/.j/tasks/list.db" \
        -hold 30s \
        -ready /tmp/lock.ready &
    HOLDER=$!
    while [ ! -f /tmp/lock.ready ]; do sleep 0.1; done
    ```
  - With the lock held, run:
    ```
    ./bin/j tasks start --from-file ./spec.md \
        --plan-requires-approval=true \
        > /tmp/start.stdout 2> /tmp/start.stderr
    echo "exit=$?"
    ```
  - Stop the lock-holder: `kill "$HOLDER"; wait "$HOLDER" 2>/dev/null`.
  - With the lock now released, list tasks:
    `./bin/j tasks --simple > /tmp/list.out 2>&1`.

Expected:
  - Exit code 0 from the `tasks start` invocation (the orchestrator
    child has been spawned; only the row write failed).
  - Stdout (`/tmp/start.stdout`) is empty: the bordered
    `RunningInBackground` banner is suppressed because the row was
    never persisted, so the task is unreachable from `j tasks`.
  - Stderr (`/tmp/start.stderr`) contains exactly one line equal to
    `■ J: cannot write to database` (orange when on a TTY) and does
    NOT contain the legacy `J: warning: tasks db: …timeout` wording.
  - After releasing the lock, `tasks --simple` (`/tmp/list.out`) is
    exactly `J: no tasks` because the row was never persisted to
    bbolt.
