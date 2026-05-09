SHELL := /bin/bash

BIN_DIR := bin
BIN     := $(BIN_DIR)/j

.PHONY: build clean coverage test e2e race lint lint-fix install-hooks

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/spacelions/j/internal/cli/version.Version=$(VERSION)"

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN) ./cmd/j

clean:
	rm -rf $(BIN_DIR) cover.out

test:
	go test ./internal/... ./cmd/...

e2e:
	go test ./testcases/...

race:
	go test -race ./...

lint:
	go tool golangci-lint run ./...

lint-fix:
	go tool golangci-lint run --fix ./...

install-hooks:
	@go tool lefthook install --reset-hooks-path

coverage:
	@set -euo pipefail; \
	coverage_pkgs=$$(go list ./internal/... | \
		rg -v '/internal/testutil$$'); \
	go test -covermode=atomic -coverprofile=cover.out $$coverage_pkgs; \
	total=$$(go tool cover -func=cover.out | awk '/^total:/ {print $$3}'); \
	echo "total coverage: $$total"; \
	branch_num=0; branch_den=0; \
	while IFS= read -r dir; do \
		if ! out=$$(go tool gobco -branch "$$dir" 2>&1); then \
			printf '%s\n' "$$out"; \
			echo "branch coverage skipped: $$dir" >&2; \
			continue; \
		fi; \
		printf '%s\n' "$$out"; \
		ratio=$$(printf '%s\n' "$$out" | \
			awk '/^Branch coverage:/ {print $$3}'); \
		if [ -z "$$ratio" ]; then continue; fi; \
		num=$${ratio%/*}; den=$${ratio#*/}; \
		branch_num=$$((branch_num + num)); \
		branch_den=$$((branch_den + den)); \
	done < <(go list -f '{{.Dir}}' $$coverage_pkgs); \
	branch_pct=$$(awk -v num="$$branch_num" -v den="$$branch_den" 'BEGIN { \
		if (den == 0) { print "100.0"; exit } \
		printf "%.1f", num * 100 / den; \
	}'); \
	echo "branch coverage: $$branch_pct% ($$branch_num/$$branch_den)"; \
	below=$$(go tool cover -func=cover.out | \
		awk '$$NF != "100.0%" && !/^total:/ {print}'); \
	below=$$(printf '%s\n' "$$below" | \
		rg -v -f coverage.allowlist | rg -v '^$$' || true); \
	if [ -n "$$below" ]; then \
		echo "the following non-allowlisted symbols are below 100% coverage:" >&2; \
		echo "$$below" >&2; \
		exit 1; \
	fi
