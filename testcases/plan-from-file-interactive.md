Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.
  - Have `cursor-agent` (or another supported coding-agent backend) on
    PATH and logged in.
  - Drop a small markdown task description at `task.md`.

Steps (MANUAL — requires a TTY and a real coding-agent backend):
  - Run `./bin/j plan -f task.md` (interactive picker selects tool /
    model). Drive the huh tool / model picker. The cursor TUI opens in
    plan mode.
  - Inside cursor, allow the planner instruction to run, then exit.

Expected:
  - The agent saves `requirements.md` and `plan.md` under
    `.j/tasks/<id>/`.
  - The first non-empty line of `.j/tasks/<id>/requirements.md` is a
    one-line summary of the task — NOT `# Requirements`, and NOT any
    other heading.
  - `./bin/j tasks` lists the new row with status `plan-done` and the
    one-line summary as the row's summary text.

Manual: yes (drives huh pickers and the cursor TUI).
