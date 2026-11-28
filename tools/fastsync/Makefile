# fastsync: genesis-sync tooling. Own module (own go.mod), outside the
# celestia-app go.work; GOWORK=off keeps builds hermetic to this go.mod.
# fastindex46 is a separate module: it needs the SDK v0.46 store of app v1-v3.

BUILDDIR := build
GO       := GOWORK=off go

.PHONY: build test tidy clean help

## build: build every command into ./build
build:
	$(GO) build -trimpath -o $(BUILDDIR)/ ./cmd/...
	cd fastindex46 && $(GO) build -trimpath -o ../$(BUILDDIR)/fastindex46 .

## test: run the tests
test:
	$(GO) test ./...
	cd fastindex46 && $(GO) test ./...

## tidy: sync go.mod/go.sum
tidy:
	$(GO) mod tidy
	cd fastindex46 && $(GO) mod tidy

## clean: remove build artifacts
clean:
	rm -rf $(BUILDDIR)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
