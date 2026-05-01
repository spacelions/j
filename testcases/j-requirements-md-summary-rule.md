Prerequisites:
  - `internal/coding-agents/cursor/cursor.go::buildPlanPrompt` non-resume
    suffix instructs the agent that the saved `requirements.md` MUST start
    with a one-line summary (no `# Requirements` heading).

Steps:
  - Read `buildPlanPrompt`. The non-resume branch (`req.Resume == false`)
    must produce a prompt whose suffix says:
      "The first line of this file MUST be a concise one-line summary"
      "do NOT use `# Requirements` (or any other heading) as the first line"
  - Run `go test ./internal/coding-agents/cursor/ -run TestPlan_Interactive$ -v`.
    Expect PASS — the test asserts the prompt contains the substring
    `one-line summary`.

Acceptance:
  - The non-resume planner save instruction explicitly forbids
    `# Requirements` as the first line and requires a one-line summary
    (AC#4).
