# llama-tui Makefile

BINARY     := llama-tui
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-X github.com/AresiusXP/llama-tui/cmd.Version=$(VERSION)"
BUILD_DIR  := ./dist

.PHONY: all build run lint test clean release

all: build

## build: compile the binary for the current platform
build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .

## run: build and run
run: build
	$(BUILD_DIR)/$(BINARY)

## dev: run directly with go run (faster iteration)
dev:
	go run $(LDFLAGS) .

## lint: run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run ./...

## test: run all tests
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## release: build release binaries for all platforms via goreleaser
release:
	goreleaser release --clean

## snapshot: test goreleaser build without publishing
snapshot:
	goreleaser release --snapshot --clean

## tidy: tidy go modules
tidy:
	go mod tidy

## help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /' | column -t -s ':'
