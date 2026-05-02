Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.
  - Drop a small markdown task description at `task.md`.

Steps:
  - Run `./bin/j plan --tool=ghost --model=opus -f task.md`.

Expected:
  - Exit code is non-zero.
  - Stderr contains `unknown tool "ghost"`.
  - The error fires before any agent invocation: no `requirements.md` /
    `plan.md` is written under `.j/tasks/`, no task row is appended, no
    `CheckLogin` prompt is launched, and the planner bucket remains
    untouched. (Confirm with `./bin/j tasks` showing no new task and
    `./bin/j settings` not containing any `planner.tool` / `planner.model`
    rows.)
