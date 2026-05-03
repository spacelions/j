Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings set planner.tool=cursor worker.model=sonnet`.
  - Run `./bin/j settings reset planner,worker.model`.
    (one positional arg: the literal string `planner,worker.model`,
    no whitespace separator between names.)
  - Run `./bin/j settings`.

Expected:
  - The `reset` invocation exits with code 0 and prints exactly one
    line:

        unset planner,worker.model

    The arg is parsed by splitting on the FIRST `.`, yielding
    bucket=`planner,worker` (literal comma) and key=`model`. No such
    bucket exists, so the underlying `s.Delete` is a no-op success.
  - Because the comma is NOT a separator, the real `planner.tool`
    and `worker.model` keys are STILL present after the reset:

        [planner]
          tool = cursor

        [worker]
          model = sonnet

  - To actually unset both, the user must use whitespace as the
    separator: `./bin/j settings reset planner worker.model`.

Pins: requirement "Whitespace is the ONLY separator — `,` and `;` are
NOT recognized as separators (they remain part of the literal target
string and will fail validation if they leave the bucket or key
empty)".
