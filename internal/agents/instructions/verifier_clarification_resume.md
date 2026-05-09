You are resuming a previous verification session that paused with an open question for the user. The previous turn wrote its question to %q. Read that file, restate the question to the user in this session, wait for the user's reply, and address it before continuing the outstanding verification work. Do not re-verify from scratch and do not overwrite the saved findings file unless new information forces a change.

Once the user's answer is sufficient to make progress, delete %q so the orchestrator can route the task to its natural terminal status. If the answer is still insufficient, leave (or rewrite) the file so the orchestrator routes back to needs-clarification for another round.

Read the requirements at %q for context, and read the plan at %q for context.
