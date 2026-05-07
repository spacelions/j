# j

`j` is the **J Harness CLI**: a local tool that ties together **plan → work → verify** for tasks under a per-project `.j/` directory (BoltDB task list, task workspaces, and coding-agent backends such as Cursor).

## Prerequisites

- **Go** matching [`.go-version`](.go-version) (see [`go.mod`](go.mod)).
- For real plan/work/verify runs: a supported **coding agent** on `PATH` (for example `cursor-agent`), installed and authenticated the way your backend expects.
- A **TTY** when using interactive pickers (huh forms); non-interactive flows use flags and IDs.

## Build and install (from source)

```bash
make          # builds ./bin/j
./bin/j --help
```

Other useful targets: `make test`, `make race`, `make clean`, `make coverage`, `make install-hooks` (Lefthook pre-commit).

## Using `j` in any project

Commands are scoped to the **current working directory**: each project gets its own `<cwd>/.j/`.

### Let's start!

1. **Initialize the project** with `j init`. That creates `.j/`, the task store, and settings under the directory you run it in. Typical non-interactive setup:

   ```bash
   j init --yes --must-read=AGENTS.md
   ```

   Adjust `--must-read` to the files coding agents must read first, or use `--must-read=""` for an explicit empty list. See `j init --help` for `--plan-requires-approval` and behavior when `.j/` already exists.

2. **Ask the customer to write a markdown file** that captures the ask: task description, acceptance criteria, links, and so on. Save it in the project, for example `task.md`.

3. **Run `j tasks start`** from a shell in that project (with `j` on `PATH`). Point at your file with **`--from-file` / `-f`**, or omit `-f` to use the same interactive **source** picker as `j plan`. You will be prompted to choose **tool and model** for the **planner**, **worker**, and **verifier** if anything is still unset. Optional **`--plan-requires-approval`** / env **`TASKS_START_PLAN_REQUIRES_APPROVAL`** overrides the project’s `plan_requires_approval` for that run. The command returns after starting a **detached background process** that continues **plan → work → verify**; that process appends stdout/stderr to **`<cwd>/.j/tasks/<id>/agent.log`**.

4. **Run `j tasks`** (no subcommand) to print a tabular status list from `list.db` (ID, STATUS, TOOL, MODEL, SUMMARY). Rows can refresh after a background run has exited.

5. **Stay in the loop.** When a phase needs you again—or you want to drive the next step—run **`j tasks continue`** (interactive task picker on a TTY, or **`--from-task <id>`** to pick a row without the picker). `continue` **dispatches on status** to the right `plan` / `work` / `verify` run or resume. Already-finished tasks print `J: task <id> already finished` and stop. Repeat **list → continue** until the task reaches **failed** / **completed** or you stop work.

### Plan a task (manual, step-by-step)

Use this when you want to drive **planning alone** or mix phases by hand instead of **`j tasks start`** / **`continue`** (see **Let's start!** above).

```bash
j plan -f path/to/task.md          # or interactive source selection
j plan resume --help                 # resume an in-flight plan session
```

Artifacts land under `.j/tasks/<id>/` (for example `requirements.md`, `plan.md`).

### Run the coder against a planned task

```bash
j work --help
j work resume --help
```

### Verify after work is done

```bash
j verify --help
j verify resume --help
```

The verifier loop expects a final line in findings of exactly `VERDICT: PASS` or `VERDICT: FAIL`.

### Other commands

| Command | Purpose |
|--------|---------|
| `j tasks` | List tasks and lifecycle helpers (`start`, `continue`, `enter`, `discard`, …); **`start` / `continue`** are in **Let's start!** above. See `j tasks --help`. |
| `j settings` | List / set / reset keys in the local j store (`j settings --help`) |

End-to-end checklists for manual TTY flows live under [`testcases/`](testcases/).

---

## Working locally on **this** repository

Use a **separate clone or directory** for dogfooding `j` so your experiments under `.j/` do not clutter the harness repo itself, unless you intend to track `.j/` for debugging.

### Everyday loop

```bash
make test                 # same as CI (`go test ./...`)
make race                 # optional race detector
```

Before committing, run **`make install-hooks`** once so Lefthook runs the same checks locally as in pre-commit (including the **300-line file cap** and `make test`).

`make coverage` runs `internal/...` tests with a coverage profile and enforces an allowlist: anything not at 100% must be explicitly listed in the `Makefile` regex allowlist, or the target fails.

### Project conventions

See [`AGENTS.md`](AGENTS.md): high test coverage, no test-only packages (use `internal/testutil`), prefer allowlists over broad seams, and keep **non-test** source files **≤ 300 lines**.

### Where things live

| Area | Role |
|------|------|
| [`cmd/j`](cmd/j) | `main` entrypoint |
| [`internal/cli`](internal/cli) | Cobra commands (`plan`, `work`, `verify`, `tasks`, …) |
| [`internal/workflow`](internal/workflow) | Planner / coder / verifier wiring and prompts |
| [`internal/coding-agents`](internal/coding-agents) | Backends (Cursor, Claude, …) |
| [`internal/store`](internal/store) | BoltDB task and settings persistence |
| [`testcases/`](testcases/) | Human-readable manual test steps |

### CI

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs `make test` on push and pull requests.
