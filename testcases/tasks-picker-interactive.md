Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`. Confirm
    the `.j/` folder exists with `test -d .j && echo ok`.
  - Seed at least two tasks (run `./bin/j plan -f task.md` twice with
    different markdown bodies, or use any other path that produces
    two `plan-done` rows).

Steps (MANUAL — requires a TTY):
  - Run `./bin/j tasks discard` (no `--id`). The huh picker lists every
    seeded task; pick one. The confirmation prompt appears (no `--yes`);
    accept with Enter / `y`.
  - Run `./bin/j tasks enter` (no `--id`). The huh picker reappears;
    pick a task. A subshell rooted at `<cwd>/.j/tasks/<id>/` is spawned;
    `pwd` confirms; `exit` returns to the original shell.
  - Run `./bin/j tasks enter --print` (no `--id`). The picker appears
    again; pick a task. Stdout prints the absolute path to the task
    directory and the command exits 0 without spawning a subshell.

Expected:
  - The `discard` flow removes the chosen row from `j tasks` output and
    deletes `<cwd>/.j/tasks/<id>/` on disk; abort (Esc / Ctrl-C) exits
    0 and leaves the row intact.
  - The `enter` subshell drops the user inside the chosen task dir.
  - `enter --print` writes the absolute path to stdout and does not
    spawn a subshell.

Manual: yes (drives huh pickers and a subshell).
