Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes`
    (no `--mustread` flag — this test exercises the prompt path).

Steps (MANUAL — requires a TTY):
  - Run `./bin/j tasks` (or any preflight-gated subcommand other
    than `init`). The "Files every agent must read first" form
    should appear.
  - Press enter without typing anything (blank submission).
  - Run `./bin/j tasks` again.
  - Run `./bin/j settings`.

Expected:
  - The first run accepts the empty submission silently and persists
    the empty string (does NOT reject blank input).
  - The second `j tasks` run does NOT re-prompt — empty value with
    `set=true` skips the question per the requirement
    "if user leaves it blank, then it is `mustread=`".
  - The `j settings` listing renders, under the `[project]` section
    header, a row that reads exactly `  mustread = ` (two-space
    indent, key present, value empty). The trailing space is
    significant: the rendering is `  <key> = <value>`, and an
    empty value yields no characters after `= `.

Manual: yes (drives the huh input form).
