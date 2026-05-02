Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set project.mustread=path/one;path/two`
    (quote the value in the shell so the `;` is not interpreted).
  - Run `./bin/j settings set planner.tool=cursor planner.model=opus planner.interactive=false`.
  - Run `./bin/j settings`.

Expected:
  - Exit code 0.
  - Stdout matches the user's example layout exactly:

    [project]
      mustread = path/one;path/two
    
    [planner]
      interactive = false
      model = opus
      tool = cursor
    
    [coder]
    
    [verifier]

  - Entries inside `[planner]` are alphabetical by key
    (`interactive`, `model`, `tool`).
  - Each entry is indented with exactly two spaces and uses `key = value`
    (raw value, no quoting).
  - Sections are separated by exactly one blank line; no trailing blank
    line after `[verifier]`.
