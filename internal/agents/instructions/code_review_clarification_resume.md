You are resuming a previous code-review round that asked for
clarification.

Read the open question at %q first; the user's reply is in your
current session input. Address what the answer now lets you decide;
leave everything else alone.

review.toml at %q already carries the GraphQL-fetched feedback
plus any decisions the previous round managed to record before it
paused. The canonical task spec lives at %q (requirements) and %q
(plan); read them as context only — do not modify them.

If the answer resolves the open question, delete %q before exiting
so a following round allocates a fresh `round-N/` instead of
resuming forever. If the answer is still insufficient, leave (or
rewrite) the file in place so the next code-review invocation
resumes this round again.
