Read review.toml at %q. The canonical task spec lives at %q
(requirements) and %q (plan); read them as context only — do not
modify them.

review.toml is pre-populated before this turn starts: the `[pr]`
block and every `[[items]]` row carry the GraphQL-fetched feedback
(source_id, kind, author, body, thread_id, path, line, is_outdated,
has_j_reply). Your job is to APPEND decisions to each existing
item — do not add or remove rows, and never change the fetched
fields.
