Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.
  - Have `cursor-agent` (or another supported coding-agent backend) on
    PATH and logged in.
  - Drop a small markdown task description at `task.md`.
  - Pre-seed the planner bucket with non-default values so the test can
    detect any accidental overwrite:
      `./bin/j settings set planner.tool=cursor planner.model=stored-model`.

Steps (MANUAL — needs a real coding-agent backend):
  - Run `./bin/j plan --tool=cursor --model=opus -f task.md`. Drive the
    cursor TUI minimally so the agent terminates.
  - After the agent exits, run `./bin/j settings`.

Expected:
  - The cursor TUI launches with model = `opus` (the explicit override).
  - `./bin/j settings` still shows `planner.tool = cursor` AND
    `planner.model = stored-model` — the bucket is left exactly as it
    was before the run. The explicit-flag path must not persist its
    arguments back to the role bucket.
  - No huh tool/model picker is shown (the prompt branch is bypassed).

Manual: yes (drives a real coding-agent backend).
