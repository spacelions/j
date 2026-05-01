Prerequisites:
  - `internal/coding-agents/cursor/cursor.go` `Plan` / `Work` methods branch
    on `req.Resume`; resume runs build the prompt via
    `prompts.BuildPlannerResume` / `prompts.BuildCoderResume`.

Steps:
  - Read `cursor.Plan`. With `req.Resume == true`, `prompt` MUST come from
    `prompts.BuildPlannerResume(req.FromFilePath, req.Body)`. The
    `Save ... Then exit.` save-from-scratch suffix MUST be skipped.
  - Read `cursor.Work`. With `req.Resume == true` (and `FixFindings` empty),
    `prompt` MUST come from
    `prompts.BuildCoderResume(req.PlanPath, req.Body, req.Worktree)`. The
    full `coder.Instruction` body MUST NOT appear in the prompt.
  - Run `go test ./internal/coding-agents/cursor/ -run "TestPlan_Interactive_Resume|TestWork_Interactive_Resume|TestWork_FixFindings_BeatsResume" -v`.
    Expect PASS — argv assertions confirm `--resume <id>`, the resume
    prompt's marker words, and the absence of the planner/coder
    instruction body and the save-suffix.

Acceptance:
  - With `Resume=true`, both `cursor.Plan` and `cursor.Work` build the
    resume-only prompt (mentions previous/check/continue) and skip the
    full instruction body and save suffix; argv shape (`--resume <id>`,
    `--mode plan` for plan, `--model`, `--workspace`) is unchanged
    (AC#2 / AC#5c).
