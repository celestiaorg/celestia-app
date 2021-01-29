PACKAGES=$(shell go list ./... | grep -v '/simulation')
COMMIT := $(shell git log -1 --format='%H')
DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf

all: install

mod:
	@go mod tidy

pre-build:
	@echo "Fetching latest tags"
	@git fetch --tags

build: mod pre-build
	@go get -u github.com/gobuffalo/packr/v2/packr2
	@cd ./cmd/lazyledger-appd && packr2
	@mkdir -p build/
	@go build -o build/ ./cmd/lazyledger-appd
	@packr2 clean
	@go mod tidy

install: go.sum
		@echo "--> Installing lazyledger-appd"
		@go install -mod=readonly ./cmd/lazyledger-appd

go.sum: mod
		@echo "--> Ensure dependencies have not been modified"
		GO111MODULE=on go mod verify

test:
	@go test -mod=readonly $(PACKAGES)

proto-gen:
	$(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace tendermintdev/sdk-proto-gen sh ./scripts/protocgen.sh

###############################################################################
###                           Tests & Simulation                            ###
###############################################################################
# The below include contains the tools target.
include contrib/devtools/Makefile
include contrib/devtools/sims.mk

test: test-unit test-build

test-all: check test-race test-cover

test-unit:
	@VERSION=$(VERSION) go test -mod=readonly -tags='ledger test_ledger_mock' ./...

test-race:
	@VERSION=$(VERSION) go test -mod=readonly -race -tags='ledger test_ledger_mock' ./...

benchmark:
	@go test -mod=readonly -bench=. ./...

test-cover:
	@export VERSION=$(VERSION); bash -x contrib/test_cover.sh
.PHONY: test-cover



