Prerequisites:
  - `codingagents.PlanRequest` and `codingagents.WorkRequest` carry a
    `Resume bool` field (`internal/coding-agents/agent.go`).
  - `internal/cli/plan/resume.go::RunResume` and
    `internal/cli/work/resume.go::RunResume` set `Resume: true` on the
    request handed to `agent.Plan` / `agent.Work`.

Steps:
  - Read both `RunResume` bodies and confirm the literal `Resume: true` line
    is present on the request struct passed to the agent.
  - Run `go test ./internal/cli/plan/ -run TestRunResume_FromTaskHappyPath -v`.
    Expect PASS — the assertion `if !agent.lastReq.Resume` proves
    propagation through `RunResume`.
  - Run `go test ./internal/cli/work/ -run TestRunResume_Work_FromTaskHappyPath -v`.
    Expect PASS — symmetric assertion for the work side.

Acceptance:
  - Both `RunResume` callers propagate `Resume=true` end-to-end (AC#3 / AC#5d).
