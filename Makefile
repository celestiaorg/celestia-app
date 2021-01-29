PACKAGES=$(shell go list ./... | grep -v '/simulation')
COMMIT := $(shell git log -1 --format='%H')

# don't use build flags until verions start being used.
# VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
# ldflags = -X github.com/lazyledger/cosmos-sdk/version.Name=lazyledger-app \
# 	-X github.com/lazyledger/cosmos-sdk/version.ServerName=lazyledger-appd \
# 	-X github.com/lazyledger/cosmos-sdk/version.Version=$(VERSION) \
# 	-X github.com/lazyledger/cosmos-sdk/version.Commit=$(COMMIT) 

# BUILD_FLAGS := -ldflags '$(ldflags)'

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
	@bash ./scripts/protocgen.sh

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



