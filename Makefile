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
		-e 'internal/cli/plan/target\.go:[0-9]+:[[:space:]]+resolveTarget[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+AskTarget[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectSource[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectTool[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+SelectModel[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+choose[[:space:]]' \
		-e 'internal/cli/plan/ui\.go:[0-9]+:[[:space:]]+run[[:space:]]' \
		-e 'internal/cli/work/target\.go:[0-9]+:[[:space:]]+resolveTarget[[:space:]]' \
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
