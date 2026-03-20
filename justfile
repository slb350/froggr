set shell := ["bash", "-cu"]
set dotenv-load := true

export PATH := env("HOME") / "go/bin:" + env("PATH")

fmt:
    goimports -w .
lint:
    golangci-lint run ./...
test:
    go test ./... -race -count=1
check: fmt lint test
build:
    go build -o bin/froggr ./cmd/froggr
