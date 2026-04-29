SHELL := /bin/bash

BIN_DIR := bin
BIN     := $(BIN_DIR)/j

.PHONY: build clean coverage test race

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/j

clean:
	rm -rf $(BIN_DIR) cover.out

test:
	go test ./...

race:
	go test -race ./...

coverage:
	@set -euo pipefail; \
	go test -covermode=atomic -coverprofile=cover.out ./internal/...; \
	total=$$(go tool cover -func=cover.out | awk '/^total:/ {print $$3}'); \
	echo "total coverage: $$total"; \
	below=$$(go tool cover -func=cover.out | awk '$$NF != "100.0%" && !/^total:/ {print}'); \
	below=$$(printf '%s\n' "$$below" | grep -Ev \
		-e 'internal/config/config\.go:[0-9]+:[[:space:]]+Init[[:space:]]' \
		-e 'internal/workflow/workflow\.go:[0-9]+:[[:space:]]+Run[[:space:]]' \
		-e 'internal/cli/run/run\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/cli/web/web\.go:[0-9]+:[[:space:]]+New[[:space:]]' \
		-e 'internal/util/mdfile/mdfile\.go:[0-9]+:[[:space:]]+Resolve[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+AskTarget[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectSource[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		-e 'internal/store/lazy\.go:[0-9]+:[[:space:]]+OpenDefault[[:space:]]' \
		-e 'internal/store/lazy\.go:[0-9]+:[[:space:]]+OpenTaskLog[[:space:]]' \
		-e 'internal/cli/settings/cmd\.go:[0-9]+:[[:space:]]+withOpenStore[[:space:]]' \
		-e 'internal/cli/settings/list\.go:[0-9]+:[[:space:]]+runList[[:space:]]' \
		-e 'internal/cli/settings/set\.go:[0-9]+:[[:space:]]+runSet[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+runResetFull[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+readConfirmationLine[[:space:]]' \
		-e 'internal/cli/settings/reset\.go:[0-9]+:[[:space:]]+runResetOneKey[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultDir[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+IsEmpty[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultPath[[:space:]]' \
		-e 'internal/store/store\.go:[0-9]+:[[:space:]]+DefaultTasksPath[[:space:]]' \
		-e 'internal/store/tasks\.go:[0-9]+:[[:space:]]+PutTask[[:space:]]' \
		-e 'internal/cli/plan/tasklog\.go:[0-9]+:[[:space:]]+beginPlanTask[[:space:]]' \
		-e 'internal/cli/plan/plan\.go:[0-9]+:[[:space:]]+cursorResumeChatID[[:space:]]' \
		-e 'internal/coding-agents/cursor/cursor\.go:[0-9]+:[[:space:]]+CreateChatID[[:space:]]' \
		-e 'internal/cli/work/tasklog\.go:[0-9]+:[[:space:]]+beginWorkTask[[:space:]]' \
		-e 'internal/cli/work/work\.go:[0-9]+:[[:space:]]+cursorResumeChatID[[:space:]]' \
		-e 'internal/cli/tasks/cmd\.go:[0-9]+:[[:space:]]+listTasks[[:space:]]' \
		-e 'internal/cli/tasks/cmd\.go:[0-9]+:[[:space:]]+writeTasks[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+AskTarget[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/work/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		|| true); \
	below=$$(printf '%s\n' "$$below" | sed '/^$$/d'); \
	if [ -n "$$below" ]; then \
		echo "the following non-allowlisted symbols are below 100% coverage:" >&2; \
		echo "$$below" >&2; \
		exit 1; \
	fi
