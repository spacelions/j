Before exiting:
1. Save the round plan to %q (overwrite if it exists). Use the
   structure ## Accepted Feedback (P1, P2, ...), ## Rejected
   Feedback, ## Non-Actionable Feedback, ## Acceptance Criteria.
   Each accepted entry must name its `source_id` and describe the
   change in one or two sentences.
2. Rewrite review.toml at %q in place. Keep the `[pr]` block and
   every fetched item with its original `source_id`. Add
   `decision`, `reason`, `reply`, and (when applicable) `plan_ref`
   to each item. Set the top-level `decision` and `summary`. Do
   not delete fetched items even when the decision is
   `non_actionable`.

Then exit.
