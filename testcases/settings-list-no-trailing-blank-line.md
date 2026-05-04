Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --must-read=`.

Steps:
  - Run `./bin/j settings | wc -l`.
  - Run `./bin/j settings | tail -c 1 | od -c | head -1`.
  - Run `./bin/j settings | tail -c 2 | od -c | head -1`.

Expected:
  - `wc -l` returns 9 (the four headers + the seeded `must_read = ` row +
    `plan_requires_approval = true` + three blank-separator lines = 9
    newlines).
  - `tail -c 1 | od -c` shows that the very last byte is a single newline
    (`\n`).
  - `tail -c 2 | od -c` shows the last two bytes are `]` then `\n` —
    proving the file ends with `[verifier]\n` and NOT with `[verifier]\n\n`
    (no trailing blank line).
