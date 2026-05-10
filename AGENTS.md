## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. Project constraints
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

- Constraints on file/method/line
  - Every non-test file must be ≤ 300 lines.
  - Each method must be <= 80 lines.
  - Each method must be <=6 parameters.
  - Each line must be <= 80 characters.

## 6. Skills
  - Golang best-practice skills are pinned in skills-lock.json
    (source: samber/cc-skills-golang).
  - Install locally: open Claude Code, run `/install-skills` or let
    the harness auto-install from the lock file on first use.
  - The installed files land in .agents/ which is gitignored.
  - skills-lock.json must be committed and kept up to date when skills
    are added or upgraded.
