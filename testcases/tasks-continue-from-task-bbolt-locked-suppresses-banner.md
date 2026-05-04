Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - Build the lock-holder + task-seeder helpers that open the bbolt
    task DB. The helpers live outside the repo to avoid a test-only
    dependency. Lock-holder source: see
    `tasks-start-bbolt-locked-suppresses-banner.md`. Task-seeder source:
    ```
    mkdir -p /tmp/verify-bbolt-lock/seed
    cat > /tmp/verify-bbolt-lock/seed/main.go <<'EOF'
    package main

    import (
        "encoding/json"
        "flag"
        "fmt"
        "os"
        "time"

        bolt "go.etcd.io/bbolt"
    )

    type Task struct {
        ID                 string     `json:"id"`
        Status             string     `json:"status"`
        InvokedTool        string     `json:"invoked_tool,omitempty"`
        InvokedModel       string     `json:"invoked_model,omitempty"`
        Summary            string     `json:"summary,omitempty"`
        PlanResumeCursor   string     `json:"plan_resume_cursor,omitempty"`
        PlanBeginAt        *time.Time `json:"plan_begin_at,omitempty"`
        PlanEndAt          *time.Time `json:"plan_end_at,omitempty"`
    }

    func main() {
        path := flag.String("path", "", "path to list.db")
        id := flag.String("id", "", "task id")
        status := flag.String("status", "plan-done", "status")
        flag.Parse()
        if *path == "" || *id == "" {
            fmt.Fprintln(os.Stderr, "seedtask: -path and -id required"); os.Exit(2)
        }
        db, err := bolt.Open(*path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
        if err != nil { fmt.Fprintln(os.Stderr, "seedtask: open:", err); os.Exit(1) }
        defer db.Close()
        now := time.Now().UTC().Add(-time.Hour)
        end := now.Add(30 * time.Minute)
        row := Task{
            ID: *id, Status: *status,
            InvokedTool: "cursor", InvokedModel: "sonnet-4",
            Summary: "seed task", PlanResumeCursor: "p",
            PlanBeginAt: &now, PlanEndAt: &end,
        }
        body, err := json.Marshal(row)
        if err != nil { fmt.Fprintln(os.Stderr, "seedtask: marshal:", err); os.Exit(1) }
        if err := db.Update(func(tx *bolt.Tx) error {
            b, err := tx.CreateBucketIfNotExists([]byte("tasks"))
            if err != nil { return err }
            return b.Put([]byte(*id), body)
        }); err != nil { fmt.Fprintln(os.Stderr, "seedtask: put:", err); os.Exit(1) }
    }
    EOF
    (cd /tmp/verify-bbolt-lock && go build -o seedtask ./seed)
    ```
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
  - Seed every agent bucket:
    ```
    ./bin/j settings set planner.tool=cursor planner.model=sonnet-4 \
                       worker.tool=cursor worker.model=sonnet-4 \
                       verifier.tool=cursor verifier.model=sonnet-4
    ```
  - Seed a `plan-done` task row directly into bbolt and stage its
    requirements/plan files (the orchestrator's resume reads them):
    ```
    TASK_ID=01HVERIFYCONTINUE0000000000
    mkdir -p ".j/tasks/$TASK_ID"
    printf '# req\n' > ".j/tasks/$TASK_ID/requirements.md"
    printf '1. step\n' > ".j/tasks/$TASK_ID/plan.md"
    /tmp/verify-bbolt-lock/seedtask \
        -path "$(pwd)/.j/tasks/list.db" \
        -id "$TASK_ID" \
        -status plan-done
    ```

Steps:
  - Hold the bbolt lock in the background:
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
    ./bin/j tasks continue --from-task 01HVERIFYCONTINUE0000000000 \
        > /tmp/cont.stdout 2> /tmp/cont.stderr
    echo "exit=$?"
    ```
  - Stop the lock-holder: `kill "$HOLDER"; wait "$HOLDER" 2>/dev/null`.

Expected:
  - Exit code 0.
  - Stdout (`/tmp/cont.stdout`) is empty: no `RunningInBackground`
    banner — the row could not be re-stamped, so the user cannot
    reach the spawned orchestrator (if any) from `j tasks`.
  - Stderr (`/tmp/cont.stderr`) contains exactly one line equal to
    `■ J: cannot write to database` and does NOT contain the legacy
    `J: warning: tasks db: …timeout` wording or the unwrapped
    `store: open … timeout` cobra error.
