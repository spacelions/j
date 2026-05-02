Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.
  - Drop a small markdown task description at `task.md`.
  - Confirm the planner bucket is empty: `./bin/j settings` must NOT list
    a `planner.tool` or `planner.model` row.

Steps:
  - Run `./bin/j plan --tool=cursor -f task.md`.
  - Run `./bin/j plan --model=opus -f task.md`.

Expected:
  - Each invocation exits with non-zero status.
  - The first stderr contains
    `--tool given without stored model in planner` (the missing-model
    branch of `agentpick.Resolve`).
  - The second stderr contains
    `--model given without stored tool in planner` (the symmetric
    missing-tool branch).
  - Neither invocation creates a task row under `.j/tasks/`, runs
    `CheckLogin`, or writes to the planner bucket.
