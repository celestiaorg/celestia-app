# fastsync: genesis-sync tooling. Own module (own go.mod), outside the
# celestia-app go.work; GOWORK=off keeps builds hermetic to this go.mod.

BUILDDIR := build
GO       := GOWORK=off go

.PHONY: build test tidy clean help

## build: build every command into ./build
build:
	$(GO) build -trimpath -o $(BUILDDIR)/ ./cmd/...

## test: run the tests
test:
	$(GO) test ./...

## tidy: sync go.mod/go.sum
tidy:
	$(GO) mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BUILDDIR)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
