MAKEFLAGS := --jobs=$(shell nproc)

.PHONY: all test lint lint-fix mod-tidy vet golangci-lint staticcheck fix govulncheck update help

all: lint test

test:
	go test -short -race ./...
	go test ./...

lint: mod-tidy vet staticcheck golangci-lint fix govulncheck

lint-fix:
	go mod tidy
	golangci-lint run --fix
	go fix ./...
	$(MAKE) lint

mod-tidy:
	go mod tidy -diff

vet:
	go vet ./...

golangci-lint:
	golangci-lint config verify
	golangci-lint run

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fix:
	go fix -diff ./...

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

update:
	go get -v -u ./...
	go mod tidy

help:
	@echo "make           # Run linters and tests"
	@echo "make test      # Run tests"
	@echo "make lint      # Run linters"
	@echo "make lint-fix  # Run linters and fix issues"
	@echo "make update    # Update Go dependencies"
