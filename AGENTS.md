## 1. How to organize code

When you are planning or writing code, think twice about where that
piece of code should sit:

| Folder | What belongs there |
|--------|--------------------|
| `internal/agents/` | Planner, worker, verifier, and agent-phase orchestration code. |
| `internal/cli/` | Cobra command wiring and CLI presentation logic. |
| `internal/coding-agents/` | Backend adapters for Codex, Claude, Cursor, DeepSeek, and shared agent contracts. |
| `internal/lifecycle/` | Plan/work/verify lifecycle transitions, markers, Linear sync, and PR URL reaping. |
| `internal/resolver/` | Task/source resolution, must-read parsing, and verdict parsing. |
| `internal/store/` | Project settings storage, paths, and shared store helpers. |
| `internal/testutil/` | Shared test helpers. Use this instead of creating test-only packages. |
| `internal/tools/` | Wrappers for external service/tool integrations. |
| `internal/util/` | Small shared utilities that are not tied to one feature area. |

Keep tests next to the package they exercise unless the behavior spans
multiple packages or is user-visible end to end; use `testcases/` for
those broader regression tests. Do not create a new first-level
`internal/` folder unless the code has a clear ownership boundary that
does not fit the folders above.

## 2. Project constraints
- Test coverage: line coverage should be >95%.
- Do not introduce seams, use allowlist instead
- MUST not introduce a package only for testing, use testutil instead.

- Commit messages must follow:
  `<type>(<component>)[SPA-<number>]: title`, where `<type>` is one of
  `feat`, `chore`, `build`, `fix`, `style`, `docs`, or `refactor`.

- Command line tools
  - use `fd` to replace `find`
  - use `z` to replace `cd`, 
  - use `eza` to replace `ls`
  - use `rg` to replace `grep`
  - use `bat` to replace `cat`
  - use `sd` to replace `sed`

- Constraints on coding files/methods/lines, not markdown files
  - Every non-test file must be ≤ 300 lines.
  - Each method must be <= 80 lines.
  - Each method must be <=6 parameters.
  - Each line must be <= 80 characters.

## 3. Skills
  - Golang best-practice skills are pinned in skills-lock.json
    (source: samber/cc-skills-golang).
  - Install locally from the repo root:
    `pnpm dlx skills add samber/cc-skills-golang --skill '*' --yes`.
  - Installed skill files are gitignored.
  - skills-lock.json must be committed and kept up to date when skills
    are added or upgraded.
