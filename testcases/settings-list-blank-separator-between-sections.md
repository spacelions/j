Prerequisites:
  - Run `make` (compiles `./bin/j`).
  - `cd` into a fresh empty directory and run `./bin/j init --yes --mustread=`.

Steps:
  - Run `./bin/j settings`.
  - Visually (or via `awk` / a hex dump) confirm that exactly one blank
    line separates each pair of consecutive section blocks.

Expected:
  - The byte stream between the closing newline of one section block and
    the opening `[` of the next is exactly one `\n` (i.e. the separator
    is a single empty line).
  - There are NEVER two consecutive blank lines.
  - Concretely on the empty-init store, the listing is the 8-line
    sequence in `settings-list-fresh-init-renders-four-sections.md`;
    each `[…]` after the first is preceded by exactly one blank line.
