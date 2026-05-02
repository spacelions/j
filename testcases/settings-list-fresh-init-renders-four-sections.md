Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.
    Confirm the `.j/` folder exists with `test -d .j && echo ok`.

Steps:
  - Run `./bin/j settings` (no subcommand).
  - Capture stdout exactly (preserve newlines / trailing newline state).

Expected:
  - Exit code 0.
  - Stdout is exactly the following 8 lines (each header on its own line,
    a single blank line between sections, no trailing blank line; the file
    output ends with the `[verifier]` line plus its terminating newline):

    [project]
      mustread = 
    
    [planner]
    
    [worker]
    
    [verifier]

  - There must be NO trailing blank line after `[verifier]` (the bytes
    after the final newline of `[verifier]` are EOF, not another `\n`).
  - The known-section order is fixed: project → planner → worker → verifier.
