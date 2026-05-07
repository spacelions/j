## Verifier findings — refactor: hand-rolled FSM for task lifecycle

### Checklist

**FSM correctness**
- [x] `transitions` table is the single source of truth; every legal edge is present and matches the Mermaid diagram in the package comment.
- [x] `Apply` returns `IllegalTransitionError` for unlisted edges and never fires hooks.
- [x] `IsLegal` / `LegalEvents` are read-only and do not fire hooks — confirmed by `TestIsLegal_DoesNotFireHooks` / `TestApply_DoesNotFireHooks`.
- [x] `Notify` fires hooks in registration order; a panicking hook does not skip successors — confirmed by `TestNotify_PanicIsolation`.
- [x] `ResetHooksForTest` panics outside testing — confirmed by guard.
- [x] No duplicate edges in transition table — `TestTransitionTable_NoDuplicateEdges`.
- [x] Terminal state `completed` has no outgoing edges — `TestTransitionTable_TerminalStates`.
- [x] All declared `TaskStatus` values appear in the table — `TestEveryValidStatusInTransitions`.

**Lifecycle helpers**
- [x] `NewPlanTask` / `BeginPlanReuse` / `BeginWorkReuse` / `BeginVerify` all call `tasks.Apply` and `tasks.Notify` — hardcoded string assignments are gone.
- [x] `Finish` on all three lifecycle types derives the new status via `tasks.Apply` and panics on illegal edges.
- [x] `RecordBackground` is idempotent via the `closed` flag.

**tuiquit package**
- [x] `DecidePlan`, `DecideWork`, `DecideVerify` are pure, side-effect-free decision functions.
- [x] `DecideWork` two-pass: agent log scan first, then `gh pr list` shell-out.
- [x] `DecideVerify` reads last non-empty line of `verifier_findings.md`.

**continue_dispatch**
- [x] `dispatchPlanApprove` fires `EventPlanApprove` via `tasks.Apply` + `tasks.Notify` before handing off to work.
- [x] `dispatchClarification` picks the correct Resume event from `latestPhase` and fires it via `tasks.Apply`.
- [x] `dispatchByStatus` now handles `StatusPlanPendingApproval` and `StatusNeedsClarification`.

**AGENTS.md constraints**
- [x] All non-test files ≤ 300 lines.
- [x] All non-test lines ≤ 80 characters (no new violations introduced by this PR).

**Findings requiring fix (resolved before this PASS)**
- `gofmt` violation in `internal/lifecycle/plan.go:182`, `work.go:140`, `verify.go:118`: extra leading tab on `newStatus, err := tasks.Apply(from, ev)` — fixed.
- `gofmt` alignment violation in `verify.go:14`: `VerifyOutcomeSuccess` had extra padding — fixed.

**All tests**
- `go test ./...` — all packages pass (zero failures).
- `go test ./testcases/` — all integration testcases pass.

VERDICT: PASS
