.PHONY: all build install run test vet lint lint-full check clean fmt help

# ── Build variables ──────────────────────────────────────────────────────────
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.2.0-dev")
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
MODULE    := github.com/house-of-cards/hoc
LDFLAGS   := -X $(MODULE)/cmd.Version=$(VERSION) -X $(MODULE)/cmd.GitCommit=$(COMMIT) -X $(MODULE)/cmd.BuildTime=$(BUILDTIME)

# Default target
all: check build

## build: compile the hoc binary into bin/
build:
	go build -ldflags "$(LDFLAGS)" -o bin/hoc ./cmd/hoc/

## install: install hoc binary with build info
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/hoc/

## run: build and run hoc with ARGS
run: build
	./bin/hoc $(ARGS)

## test: run all tests
test:
	go test ./...

## test-cover: run tests with coverage report
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## vet: run go vet static analysis
vet:
	go vet ./...

## lint: check gofmt formatting (fails if any file is not formatted)
lint:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "ERROR: The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		echo "Run 'make fmt' to fix."; \
		exit 1; \
	fi
	@echo "gofmt: all files OK"

## fmt: format all Go source files with gofmt
fmt:
	gofmt -w .

## lint-full: run golangci-lint with project config
lint-full:
	golangci-lint run ./...

## check: run vet + lint + lint-full + test (full CI gate)
check: vet lint lint-full test

## clean: remove generated artifacts
clean:
	rm -f coverage.out
	rm -rf bin/

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
