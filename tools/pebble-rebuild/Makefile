# pebble-rebuild — offline rebuild of a fragmented Pebble store into a fresh,
# compaction-friendly one. Self-contained module (its own go.mod) so it does not
# couple to the celestia-app dependency graph.
#
# GOWORK=off keeps the build hermetic to this module's go.mod: the parent
# celestia-app go.work would otherwise refuse to build a module it doesn't list.
# CGO is disabled so the binary is statically linked and can be scp'd to an
# archival node without matching a libc / cgo-zstd toolchain.

BIN      := pebble-rebuild
BUILDDIR := build
GO       := GOWORK=off CGO_ENABLED=0 go
GOBUILD  := $(GO) build -trimpath

.PHONY: all build build-linux install test tidy clean help

all: build

## build: build the tool for the host platform into ./build
build:
	$(GOBUILD) -o $(BUILDDIR)/$(BIN) .

## build-linux: cross-compile a static linux/amd64 binary (for the archival node)
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILDDIR)/$(BIN).linux .

## install: install the tool into $GOBIN / $GOPATH/bin
install:
	$(GO) install .

## test: run the round-trip + sample tests
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
