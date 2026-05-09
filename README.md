# j

`j` is the **J Harness CLI**: a local tool that orchestrates the **plan → work → verify** lifecycle for coding tasks. It stores tasks under a per-project `.j/` directory (BoltDB task list, per-task workspaces) and shells out to coding-agent backends such as Cursor or Claude.

## Prerequisites

- **Go** matching [`.go-version`](.go-version) (see [`go.mod`](go.mod)).
- A supported **coding agent** on `PATH` for real plan/work/verify runs:
  - `cursor-agent` — run `cursor-agent login` to authenticate
  - `claude` — run `claude auth login` to authenticate
- A **TTY** when using interactive pickers (huh forms); non-interactive flows use flags and IDs.

## Build and install (from source)

```bash
make          # builds ./bin/j
./bin/j --help
```

Other useful targets: `make test`, `make race`, `make clean`,
`make coverage`, `make line-coverage`, `make branch-coverage`,
`make install-hooks` (Lefthook pre-commit).

## Using `j` in any project

Commands are scoped to the **current working directory**: each project gets its own `<cwd>/.j/`.

### Getting started

1. **Initialize the project** with `j init`. Creates `.j/`, the task store, and settings in the current directory.

   ```bash
   j init --yes --must-read=AGENTS.md
   ```

   Adjust `--must-read` to files coding agents must read first. See `j init --help` for `--plan-requires-approval` and behavior when `.j/` already exists.

2. **Write a markdown file** capturing the task: description, acceptance criteria, links, etc. Save it in the project (e.g. `task.md`).

3. **Run `j tasks start`** to create a task and launch a detached background orchestrator:

   ```bash
   j tasks start -f task.md
   ```

   Source options:
   - `-f / --from-file <path>` — load requirement from a markdown file
   - `--from-linear <ID>` — load from a Linear issue (e.g. `ENG-123`); requires `linear.api_key` in settings
   - Omit both to use the interactive source picker

   You will be prompted for **tool** and **model** for each phase if not already set in settings. The command returns immediately; the background process appends to **`<cwd>/.j/tasks/<id>/agent.log`**.

4. **Run `j tasks`** (no subcommand) to list task status. On a TTY this renders a live-updating table; use `--simple` for plain tabwriter output suitable for pipes.

5. **Stay in the loop.** When a phase needs input—or you want to drive the next step manually—run:

   ```bash
   j tasks continue                   # interactive task picker
   j tasks continue --from-task <id>  # skip the picker
   ```

   `continue` dispatches on the current task status to the right plan / work / verify run or resume. Repeat **list → continue** until the task reaches **failed** or **completed**.

### Manual phase control

Use these when you want to re-run or resume individual phases instead of the automated orchestrator.

```bash
j tasks re-plan --help       # re-run the planner on an existing task
j tasks resume-plan --help   # resume an in-flight planner session

j tasks re-work --help       # re-run the worker on an existing task
j tasks resume-work --help   # resume an in-flight worker session

j tasks re-verify --help     # re-run the verifier on an existing task
j tasks resume-verify --help # resume an in-flight verifier session
```

The verifier expects a final line of exactly `VERDICT: PASS` or `VERDICT: FAIL` in its findings.

Artifacts land under `.j/tasks/<id>/`: `requirements.md`, `plan.md`, `agent.log`, `verifier_findings.md`.

### `j tasks` subcommands

| Subcommand | Purpose |
|------------|---------|
| `start` | Create task, spawn detached orchestrator |
| `continue` | Resume or advance the current phase |
| `enter` | Open a subshell in the task's directory |
| `discard` | Discard a task, its linked git worktree, and its on-disk directory |
| `logs` | Print or tail `agent.log` |
| `show` | Render task files (`requirements`, `plan`, `clarification`, …) |
| `re-plan` / `resume-plan` | Replan or resume an interrupted planning phase |
| `re-work` / `resume-work` | Rework or resume an interrupted work phase |
| `re-verify` / `resume-verify` | Reverify or resume an interrupted verification phase |
| `orchestrate` | Run plan/work/verify sequentially (used internally by `start`) |

### Other commands

| Command | Purpose |
|---------|---------|
| `j settings` | List / set / reset keys in the local j store |
| `j run` | Launch the agent in the ADK console (interactive) |
| `j web` | Launch the ADK web UI |

### Linear integration

`j` can pull tasks from and sync state back to [Linear](https://linear.app):

```bash
j settings set linear.api_key=<your-api-key>
j tasks start --from-linear ENG-123
```

On task state changes, `j` syncs the Linear issue state and posts comments with phase transitions and the final verdict.

---

## Working locally on **this** repository

Use a **separate clone or directory** for dogfooding `j` so your experiments under `.j/` don't clutter the harness repo itself.

### Everyday loop

```bash
make test    # same as CI (`go test ./...`)
make race    # optional race detector
```

Run **`make install-hooks`** once before committing so Lefthook enforces the same checks locally as in CI (300-line file cap, `make test`).

`make coverage` is a compatibility alias for `make line-coverage`.
It runs `internal/...` tests with a coverage profile and enforces the
`coverage.allowlist`: anything not at 100% must be explicitly listed
there, or the target fails.

`make branch-coverage` reports aggregate branch coverage separately.
It is wired to a standalone GitHub Actions workflow, not the main CI
workflow.

### Project conventions

See [`AGENTS.md`](AGENTS.md): high test coverage, no test-only packages (use `internal/testutil`), prefer allowlists over broad seams, and keep **non-test** source files **≤ 300 lines**.

### Where things live

| Area | Role |
|------|------|
| [`cmd/j`](cmd/j) | `main` entrypoint |
| [`internal/cli`](internal/cli) | Cobra commands (`init`, `tasks`, `settings`, `run`, `web`) |
| [`internal/agents`](internal/agents) | Planner / worker / verifier sub-agents and embedded prompt instructions |
| [`internal/lifecycle`](internal/lifecycle) | Per-phase lifecycle markers, Linear state sync, PR URL reaping |
| [`internal/lifecycle/orchestrator`](internal/lifecycle/orchestrator) | Google ADK sequential/loop agent wiring |
| [`internal/coding-agents`](internal/coding-agents) | Coding backends: Cursor and Claude |
| [`internal/tools/linear`](internal/tools/linear) | GraphQL client for Linear issue queries and mutations |
| [`internal/resolver`](internal/resolver) | Source resolution (markdown, Linear), task lookup, verdict parsing |
| [`internal/store`](internal/store) | BoltDB task and settings persistence |
| [`testcases/`](testcases/) | Human-readable manual test steps |

### CI

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs lint,
tests, e2e, and line coverage on push and pull requests. Branch
coverage runs in
[`.github/workflows/branch-coverage.yml`](.github/workflows/branch-coverage.yml).
