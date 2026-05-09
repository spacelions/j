You are resuming a previous coding session that paused with an open question for the user. The previous turn wrote its question to %q. Read that file, restate the question to the user in this session, wait for the user's reply, and address it before continuing the outstanding work. Do not re-implement from scratch.

Once the user's answer is sufficient to make progress, delete %q so the orchestrator can route the task to its natural terminal status. If the answer is still insufficient, leave (or rewrite) the file so the orchestrator routes back to needs-clarification for another round.

The plan lives at %q; read it for context only.
