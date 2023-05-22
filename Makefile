VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf
IMAGE := ghcr.io/tendermint/docker-build-proto:latest
DOCKER_PROTO_BUILDER := docker run -v $(shell pwd):/workspace --workdir /workspace $(IMAGE)
PROJECTNAME=$(shell basename "$(PWD)")

# process linker flags
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=celestia-app \
		  -X github.com/cosmos/cosmos-sdk/version.AppName=celestia-appd \
		  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
		  -X "github.com/cosmos/cosmos-sdk/version.BuildTags=$(build_tags_comma_sep)"
ldflags += $(LDFLAGS)

BUILD_FLAGS := -ldflags '$(ldflags)'

## help: Get more info on make commands.
help: Makefile
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'
.PHONY: help

## build: Build the celestia-appd binary into the ./build directory.
build: mod
	@cd ./cmd/celestia-appd
	@mkdir -p build/
	@go build $(BUILD_FLAGS) -o build/ ./cmd/celestia-appd
	@go mod tidy -compat=1.20
.PHONY: build

## install: Build and install the celestia-appd binary into the $GOPATH/bin directory.
install: go.sum
	@echo "--> Installing celestia-appd"
	@go install -mod=readonly $(BUILD_FLAGS) ./cmd/celestia-appd
.PHONY: install

## mod: Update go.mod.
mod:
	@echo "--> Updating go.mod"
	@go mod tidy -compat=1.20
.PHONY: mod

## mod-verify: Verify dependencies have expected content.
mod-verify: mod
	@echo "--> Verifying dependencies have expected content"
	GO111MODULE=on go mod verify
.PHONY: mod-verify

## proto-gen: Generate protobuf files. Requires docker.
proto-gen:
	@echo "--> Generating Protobuf files"
	$(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace tendermintdev/sdk-proto-gen:v0.7 sh ./scripts/protocgen.sh
.PHONY: proto-gen

## proto-lint: Lint protobuf files. Requires docker.
proto-lint:
	@echo "--> Linting Protobuf files"
	@$(DOCKER_BUF) lint --error-format=json
.PHONY: proto-lint

## proto-format: Format protobuf files. Requires docker.
proto-format:
	@echo "--> Formatting Protobuf files"
	@$(DOCKER_PROTO_BUILDER) find . -name '*.proto' -path "./proto/*" -exec clang-format -i {} \;
.PHONY: proto-format

## build-docker: Build the celestia-appd docker image. Requires docker.
build-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t celestiaorg/celestia-app -f docker/Dockerfile .
.PHONY: build-docker

## lint: Run linters golangci-lint and markdownlint.
lint:
	@echo "--> Running golangci-lint"
	@golangci-lint run
	@echo "--> Running markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md'
	@echo "--> Running hadolint"
	@hadolint Dockerfile
	@echo "--> Running yamllint"
	@yamllint --no-warnings . -c .yamllint.yml

.PHONY: lint

## fmt: Format files per linters golangci-lint and markdownlint.
fmt:
	@echo "--> Running golangci-lint --fix"
	@golangci-lint run --fix
	@echo "--> Running markdownlint --fix"
	@markdownlint --fix --quiet --config .markdownlint.yaml .
.PHONY: fmt

## test: Run unit tests.
test:
	@echo "--> Running unit tests"
	@go test -mod=readonly ./...
.PHONY: test

## test-short: Run unit tests in short mode.
test-short:
	@echo "--> Running tests in short mode"
	@go test -mod=readonly ./... -short
.PHONY: test-short

## test-race: Run unit tests in race mode.
test-race:
	@echo "--> Running tests in race mode"
	@VERSION=$(VERSION) go test -mod=readonly -race -short ./...
.PHONY: test-race

## test-bench: Run unit tests in bench mode.
test-bench:
	@echo "--> Running tests in bench mode"
	@go test -mod=readonly -bench=. ./...
.PHONY: test-bench

## test-coverage: Generate test coverage.txt
test-coverage:
	@echo "--> Generating coverage.txt"
	@export VERSION=$(VERSION); bash -x scripts/test_cover.sh
.PHONY: test-coverage

## adr-gen: Download the ADR template from the celestiaorg/.github repo. Ex. `make adr-gen`
adr-gen:
	@echo "--> Downloading ADR template"
	@curl -sSL https://raw.githubusercontent.com/celestiaorg/.github/main/adr-template.md > docs/architecture/adr-template.md
.PHONY: adr-gen
