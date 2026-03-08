.PHONY: all build test vet lint check clean

# Default target
all: check build

## build: compile the hoc binary
build:
	go build ./...

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

## check: run vet + lint + test (full CI gate)
check: vet lint test

## clean: remove generated artifacts
clean:
	rm -f coverage.out

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
