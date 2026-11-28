# fastsync: genesis-sync tooling. Own module (own go.mod), outside the
# celestia-app go.work; GOWORK=off keeps builds hermetic to this go.mod.
# The NESTED commands are separate modules, pinned to the SDK store their app
# versions use (v0.46 for app v1-v3, the v0.50 store fork for app v4+).

BUILDDIR := build
GO       := GOWORK=off go
NESTED   := fastindex46 cstool46 cstool50
TESTFLAGS ?=

.PHONY: build test vet tidy clean help

## build: build every command into ./build
build:
	$(GO) build -trimpath -o $(BUILDDIR)/ ./cmd/...
	for m in $(NESTED); do (cd $$m && $(GO) build -trimpath -o ../$(BUILDDIR)/$$m .) || exit 1; done

## test: run the tests
test:
	$(GO) test $(TESTFLAGS) ./...
	for m in $(NESTED); do (cd $$m && $(GO) test $(TESTFLAGS) ./...) || exit 1; done

## vet: go vet every module
vet:
	$(GO) vet ./...
	for m in $(NESTED); do (cd $$m && $(GO) vet ./...) || exit 1; done

## tidy: sync go.mod/go.sum
tidy:
	$(GO) mod tidy
	for m in $(NESTED); do (cd $$m && $(GO) mod tidy) || exit 1; done

## clean: remove build artifacts
clean:
	rm -rf $(BUILDDIR)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
