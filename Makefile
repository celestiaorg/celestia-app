#!/usr/bin/make -f

VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf
IMAGE := ghcr.io/tendermint/docker-build-proto:latest
DOCKER_PROTO_BUILDER := docker run -v $(shell pwd):/workspace --workdir /workspace $(IMAGE)

# process linker flags
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=celestia-app \
		  -X github.com/cosmos/cosmos-sdk/version.AppName=celestia-appd \
		  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
		  -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"
ldflags += $(LDFLAGS)

BUILD_FLAGS := -ldflags '$(ldflags)'

all: install

install: go.sum
	@echo "--> Installing celestia-appd"
	@go install -mod=readonly $(BUILD_FLAGS) ./cmd/celestia-appd

go.sum: mod
	@echo "--> Verifying dependencies have expected content"
	GO111MODULE=on go mod verify

mod:
	@echo "--> Updating go.mod"
	@go mod tidy -compat=1.18

pre-build:
	@echo "--> Fetching latest git tags"
	@git fetch --tags

build: mod
	@go install github.com/gobuffalo/packr/v2/packr2@latest
	@cd ./cmd/celestia-appd && packr2
	@mkdir -p build/
	@go build $(BUILD_FLAGS) -o build/ ./cmd/celestia-appd
	@packr2 clean
	@go mod tidy -compat=1.18

proto-gen:
	@echo "--> Generating Protobuf files"
	$(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace tendermintdev/sdk-proto-gen:v0.7 sh ./scripts/protocgen.sh
.PHONY: proto-gen

proto-lint:
	@echo "--> Linting Protobuf files"
	@$(DOCKER_BUF) lint --error-format=json
.PHONY: proto-lint

proto-format:
	@echo "--> Formatting Protobuf files"
	@$(DOCKER_PROTO_BUILDER) find . -name '*.proto' -path "./proto/*" -exec clang-format -i {} \;
.PHONY: proto-format

build-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t celestiaorg/celestia-app -f docker/Dockerfile .
.PHONY: build-docker

lint:
	@echo "--> Running golangci-lint"
	@golangci-lint run
	@echo "--> Running markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md'
.PHONY: lint

fmt:
	@echo "--> Running golangci-lint --fix"
	@golangci-lint run --fix
	@echo "--> Running markdownlint --fix"
	@markdownlint --fix --quiet --config .markdownlint.yaml .
.PHONY: fmt

test:
	@echo "--> Running unit tests"
	@go test -mod=readonly ./...
.PHONY: test

test-all: test-race test-cover

test-race:
	@echo "--> Running tests with -race"
	@VERSION=$(VERSION) go test -mod=readonly -race -test.short ./...
.PHONY: test-race

test-cover:
	@echo "--> Generating coverage.txt"
	@export VERSION=$(VERSION); bash -x scripts/test_cover.sh
.PHONY: test-cover

benchmark:
	@echo "--> Running tests with -bench"
	@go test -mod=readonly -bench=. ./...
.PHONY: benchmark
