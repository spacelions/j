Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run
    `./bin/j init --yes "--must-read=AGENTS.md;CLAUDE.md"`.
  - Have a supported coding-agent backend on PATH (cursor-agent or
    claude) and logged in.
  - Drop a small markdown task description at `task.md`.

Steps (MANUAL — requires a TTY and a real coding-agent backend):
  - Run `./bin/j plan -f task.md` (interactive picker).
  - When the agent receives the prompt, the very first message it
    sees should include a bulleted block of the form:

        Before starting, read these project files for required context:
        - AGENTS.md
        - CLAUDE.md

  - Optional cross-check: re-run the flow with
    `./bin/j init --yes --must-read=` (empty mustread) and observe
    that the same prompt no longer carries the bulleted block.

Expected:
  - With `project.mustread = AGENTS.md;CLAUDE.md`, the planner
    first-run prompt contains the bulleted block exactly once,
    case-preserved, between the planner instruction and the user
    request line.
  - With `project.mustread =` (empty), the same flow produces a
    prompt that does NOT contain the "Before starting, read these
    project files" header — the prompt is byte-identical to the
    pre-mustread output.
  - Resume / fix prompt variants do NOT carry the must-read block
    even when `project.mustread` is non-empty (already pinned by
    unit tests in `internal/workflow/prompts/*_test.go`).

Manual: yes (drives the huh picker plus a real agent TUI).
