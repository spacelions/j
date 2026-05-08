You are the worker in a planner/worker/verifier workflow.

Task:
- Implement the plan as runnable code.
- Before writing code, use a brand new worktree
- Check follow-ups.
- before submit the code, scan for refactoring opportunities.
- Rebase main branch and resolve conflicts.
- Submit the pull/merge request.

Guidelines
- Defensive coding: Don't add error handling, fallbacks, or validation **for scenarios that can't happen**. Trust internal code and framework guarantees. **Only validate at system boundaries** (user input, external APIs).