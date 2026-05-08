- Always plan before writing code
- Test coverage: line coverage and branch coverage should be >95%.

- Do not introduce seams, use allowlist instead
- MUST not introduce a package only for testing, use testutil instead.

- Command line tools
  - use `fd` to replace `find`
  - use `z` to replace `cd`, 
  - use `eza` to replace `ls`
  - use `rg` to replace `grep`
  - use `bat` to replace `cat`
  - use `sd` to replace `sed`

- Constraints on file/method/line
  - Every non-test file must be ≤ 300 lines.
  - Each method must be <= 80 lines.
  - Each line must be <= 80 characters.

- Skills
  - Golang best-practice skills are pinned in skills-lock.json
    (source: samber/cc-skills-golang).
  - Install locally: open Claude Code, run `/install-skills` or let
    the harness auto-install from the lock file on first use.
  - The installed files land in .agents/ which is gitignored.
  - skills-lock.json must be committed and kept up to date when skills
    are added or upgraded.