Prerequisites:
  - `internal/workflow/prompts/{planner_prompt.go,coder_prompt.go}` defining
    `BuildPlannerResume` and `BuildCoderResume`.

Steps:
  - Inspect `BuildPlannerResume(targetPath, body)`. Confirm the rendered text
    is non-empty, mentions "previous", "check", and "continue"
    (case-insensitive), embeds `targetPath` and `body`, and does NOT inline
    `planner.Instruction`.
  - Inspect `BuildCoderResume(planPath, body, worktree)`. Same requirements
    (no `coder.Instruction`); a non-empty `worktree` appends the shared
    worktree-direction line via `appendWorktreeLine`.
  - Run `go test ./internal/workflow/prompts/ -run "TestBuildPlannerResume|TestBuildCoderResume" -v`.
    Expect PASS.
  - Diff the two builders against their non-resume counterparts: the resume
    output must differ in shape (no instruction body, different opening
    sentence).

Acceptance:
  - Both resume builders return non-empty text mentioning previous/check/
    continue, distinct from the non-resume builders, with no instruction
    body inlined (AC#2 / AC#5b).
