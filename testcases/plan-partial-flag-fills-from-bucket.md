Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.
  - Have `cursor-agent` (or another supported coding-agent backend) on
    PATH and logged in.
  - Drop a small markdown task description at `task.md`.
  - Pre-seed the planner bucket: `./bin/j settings set planner.tool=cursor`.

Steps (MANUAL — needs a real coding-agent backend):
  - Run `./bin/j plan --model=opus -f task.md`. Drive the cursor TUI
    minimally (any quick exit is fine) so the agent terminates.
  - After the agent exits, run `./bin/j settings`.

Expected:
  - The cursor TUI launches with model = `opus` (the partial flag
    forwarded the explicit half; the missing tool half came from the
    pre-seeded `planner.tool`).
  - `./bin/j settings` still shows `planner.tool = cursor` and does NOT
    show a `planner.model` row (explicit overrides are read-only with
    respect to the role bucket).

Manual: yes (drives a real coding-agent backend).
