// Package tasklog holds the CLI-layer plumbing shared by `j plan`,
// `j work`, and `j verify` for writing to the per-project task log at
// `<cwd>/.j/tasks/list.db`. Each phase owns its own lifecycle type
// (planLifecycle / workLifecycle / verifyLifecycle) because the
// status-transition rules differ per phase; the duplicated bits
// (open-with-warn, persist-with-warn, summary derivation, requirement
// sidecar lookup, agent.log filename) live here so every flow shares
// a single implementation.
//
// The helpers intentionally do not hold a bbolt handle across calls:
// every write opens the DB, puts the row, and closes before
// returning so concurrent `j tasks` invocations from another shell
// are not blocked on the OS file lock.
package tasklog

// AgentLogFileName is the per-task file that captures stdout/stderr
// of a fire-and-forget headless cursor-agent child. It lives at
// `<cwd>/.j/tasks/<id>/agent.log` and is written to each task row's
// AgentLogPath so `j tasks` and the user can find it later. All
// phases (plan / work / verify) share this filename so the reaper in
// `j tasks` can surface the log regardless of which command spawned
// the child.
const AgentLogFileName = "agent.log"
