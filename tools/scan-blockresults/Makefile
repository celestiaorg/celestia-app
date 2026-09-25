# scan-blockresults — offline report of heights missing their stored block results.
# Own module: it must link the celestia-core of the nodes it reads (v0.40.9 for
# celestia-appd v9), not the one celestia-app main uses.
#
# GOWORK=off keeps the build hermetic to this module's go.mod. CGO is disabled so
# the binary is statically linked and can be scp'd to an archival node.

BIN      := scan-blockresults
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
