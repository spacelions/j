SHELL := /bin/bash

BIN_DIR := bin
BIN     := $(BIN_DIR)/j

.PHONY: build clean coverage test race install-hooks

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/j

clean:
	rm -rf $(BIN_DIR) cover.out

test:
	go test ./...

race:
	go test -race ./...

install-hooks:
	@bash scripts/install-hooks.sh

coverage:
	@set -euo pipefail; \
	go test -covermode=atomic -coverprofile=cover.out ./internal/...; \
	total=$$(go tool cover -func=cover.out | awk '/^total:/ {print $$3}'); \
	echo "total coverage: $$total"; \
	below=$$(go tool cover -func=cover.out | awk '$$NF != "100.0%" && !/^total:/ {print}'); \
	below=$$(printf '%s\n' "$$below" | grep -Ev \
		-e 'internal/workflow/workflow\.go:[0-9]+:[[:space:]]+Run[[:space:]]' \
		-e 'internal/workflow/loadconfig\.go:[0-9]+:[[:space:]]+LoadConfig[[:space:]]' \
		-e 'internal/workflow/loadconfig\.go:[0-9]+:[[:space:]]+readSetting[[:space:]]' \
		-e 'internal/cli/run/run\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/cli/web/web\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/util/mdfile/mdfile\.go:[0-9]+:[[:space:]]+Resolve[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+AskTarget[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+AskFromFile[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectSource[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		-e 'internal/cli/initcmd/ui\.go:[0-9]+:[[:space:]]+ConfirmReset[[:space:]]' \
		-e 'internal/cli/tasks/ui\.go:[0-9]+:[[:space:]]+ConfirmDiscard[[:space:]]' \
		-e 'internal/cli/tasks/ui\.go:[0-9]+:[[:space:]]+PickTask[[:space:]]' \
		-e 'internal/cli/initcmd/cmd\.go:[0-9]+:[[:space:]]+Run[[:space:]]' \
		-e 'internal/cli/initcmd/cmd\.go:[0-9]+:[[:space:]]+seedDefaults[[:space:]]' \
		-e 'internal/cli/tasks/discard\.go:[0-9]+:[[:space:]]+RunDiscard[[:space:]]' \
		-e 'internal/cli/tasks/enter\.go:[0-9]+:[[:space:]]+RunEnter[[:space:]]' \
		-e 'internal/cli/preflight/preflight\.go:[0-9]+:[[:space:]]+ConfirmInit[[:space:]]' \
		-e 'internal/cli/preflight/preflight\.go:[0-9]+:[[:space:]]+PreRunE[[:space:]]' \
		-e 'internal/cli/settings/cmd\.go:[0-9]+:[[:space:]]+withOpenStore[[:space:]]' \
		-e 'internal/cli/settings/list\.go:[0-9]+:[[:space:]]+runList[[:space:]]' \
		-e 'internal/cli/settings/set\.go:[0-9]+:[[:space:]]+runSet[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+runResetFull[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+readConfirmationLine[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+runResetOneKey[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultDir[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+ProjectName[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+IsEmpty[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultPath[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultTasksDir[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultTasksDBPath[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+EnsureProject[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+ProjectInitialized[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+EnsureTaskDir[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+RemoveTaskDir[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+touchBoltFile[[:space:]]' \
		-e 'internal/store/task\.go:[0-9]+:[[:space:]]+PutTask[[:space:]]' \
		-e 'internal/store/persist\.go:[0-9]+:[[:space:]]+PersistWarn[[:space:]]' \
		-e 'internal/store/lifecycle_plan\.go:[0-9]+:[[:space:]]+BeginPlanReuse[[:space:]]' \
		-e 'internal/store/lifecycle_verify\.go:[0-9]+:[[:space:]]+BeginVerifyResume[[:space:]]' \
		-e 'internal/cli/plan/plan\.go:[0-9]+:[[:space:]]+openTasks[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+openTasks[[:space:]]' \
		-e 'internal/cli/verify/verify\.go:[0-9]+:[[:space:]]+openTasks[[:space:]]' \
		-e 'internal/cli/plan/plan\.go:[0-9]+:[[:space:]]+cursorResumeChatID[[:space:]]' \
		-e 'internal/cli/plan/plan\.go:[0-9]+:[[:space:]]+openSettingsStore[[:space:]]' \
		-e 'internal/cli/plan/plan\.go:[0-9]+:[[:space:]]+pickMarkdownTarget[[:space:]]' \
		-e 'internal/cli/plan/cmd\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/cli/plan/resume\.go:[0-9]+:[[:space:]]+RunResume[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+PickPlanTask[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+PickFromFile[[:space:]]' \
		-e 'internal/coding-agents/cursor/cursor\.go:[0-9]+:[[:space:]]+CreateChatID[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+cursorResumeChatID[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+openSettingsStore[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+resolveByTaskID[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+resolveFromFile[[:space:]]' \
		-e 'internal/cli/work/cmd\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/cli/work/resume\.go:[0-9]+:[[:space:]]+RunResume[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+pickTask[[:space:]]' \
		-e 'internal/cli/tasks/cmd\.go:[0-9]+:[[:space:]]+listTasks[[:space:]]' \
		-e 'internal/cli/tasks/cmd\.go:[0-9]+:[[:space:]]+writeTasks[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+AskTarget[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+AskFromFile[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+PickPlanTask[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		-e 'internal/cli/verify/cmd\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/cli/verify/verify\.go:[0-9]+:[[:space:]]+openSettingsStore[[:space:]]' \
		-e 'internal/cli/verify/verify\.go:[0-9]+:[[:space:]]+resolveByTaskID[[:space:]]' \
		-e 'internal/cli/verify/resume\.go:[0-9]+:[[:space:]]+RunResume[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+AskFromFile[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+PickWorkDoneTask[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+PickVerifyTask[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+pickTask[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/verify/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		-e 'internal/util/run/run\.go:[0-9]+:[[:space:]]+Spawn[[:space:]]' \
		-e 'internal/util/run/run\.go:[0-9]+:[[:space:]]+SpawnIn[[:space:]]' \
		-e 'internal/util/run/run\.go:[0-9]+:[[:space:]]+IsAlive[[:space:]]' \
		-e 'internal/util/mdfile/mdfile\.go:[0-9]+:[[:space:]]+ListInDir[[:space:]]' \
		|| true); \
	below=$$(printf '%s\n' "$$below" | sed '/^$$/d'); \
	if [ -n "$$below" ]; then \
		echo "the following non-allowlisted symbols are below 100% coverage:" >&2; \
		echo "$$below" >&2; \
		exit 1; \
	fi
