VERSION := $(shell echo $(shell git describe --tags 2>/dev/null || git log -1 --format='%h') | sed 's/^v//')
COMMIT := $(shell git rev-parse --short HEAD)
DOCKER := $(shell which docker)
DOCKER_BUF := $(DOCKER) run --rm -v $(CURDIR):/workspace --workdir /workspace bufbuild/buf
IMAGE := ghcr.io/tendermint/docker-build-proto:latest
DOCKER_PROTO_BUILDER := docker run -v $(shell pwd):/workspace --workdir /workspace $(IMAGE)
PROJECTNAME=$(shell basename "$(PWD)")
HTTPS_GIT := https://github.com/celestiaorg/celestia-app.git
PACKAGE_NAME          := github.com/celestiaorg/celestia-app/v2
GOLANG_CROSS_VERSION  ?= v1.22.4

# process linker flags
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=celestia-app \
		  -X github.com/cosmos/cosmos-sdk/version.AppName=celestia-appd \
		  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \

BUILD_FLAGS := -tags "ledger" -ldflags '$(ldflags)'

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
.PHONY: build

## install: Build and install the celestia-appd binary into the $GOPATH/bin directory.
install: go.sum
	@echo "--> Installing celestia-appd"
	@go install $(BUILD_FLAGS) ./cmd/celestia-appd
.PHONY: install

## mod: Update all go.mod files.
mod:
	@echo "--> Updating go.mod"
	@go mod tidy
	@echo "--> Updating go.mod in ./test/testground"
	@(cd ./test/testground && go mod tidy)
	@echo "--> Updating go.mod in ./test/interchain"
	@(cd ./test/interchain && go mod tidy)
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

## proto-check-breaking: Check if there are any breaking change to protobuf definitions.
proto-check-breaking:
	@echo "--> Checking if Protobuf definitions have any breaking changes"
	@$(DOCKER_BUF) breaking --against $(HTTPS_GIT)#branch=main
.PHONY: proto-check-breaking

## proto-format: Format protobuf files. Requires Docker.
proto-format:
	@echo "--> Formatting Protobuf files"
	@$(DOCKER_PROTO_BUILDER) find . -name '*.proto' -path "./proto/*" -exec clang-format -i {} \;
.PHONY: proto-format

## build-docker: Build the celestia-appd docker image from the current branch. Requires docker.
build-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t celestiaorg/celestia-app -f Dockerfile .
.PHONY: build-docker

## build-ghcr-docker: Build the celestia-appd docker image from the last commit. Requires docker.
build-ghcr-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t ghcr.io/celestiaorg/celestia-app:$(COMMIT) -f Dockerfile .
.PHONY: build-ghcr-docker

## publish-ghcr-docker: Publish the celestia-appd docker image. Requires docker.
publish-ghcr-docker:
# Make sure you are logged in and authenticated to the ghcr.io registry.
	@echo "--> Publishing Docker image"
	$(DOCKER) push ghcr.io/celestiaorg/celestia-app:$(COMMIT)
.PHONY: publish-ghcr-docker

## lint: Run all linters; golangci-lint, markdownlint, hadolint, yamllint.
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

## markdown-link-check: Check all markdown links.
markdown-link-check:
	@echo "--> Running markdown-link-check"
	@find . -name \*.md -print0 | xargs -0 -n1 markdown-link-check


## fmt: Format files per linters golangci-lint and markdownlint.
fmt:
	@echo "--> Running golangci-lint --fix"
	@golangci-lint run --fix
	@echo "--> Running markdownlint --fix"
	@markdownlint --fix --quiet --config .markdownlint.yaml .
.PHONY: fmt

## test: Run tests.
test:
	@echo "--> Running tests"
	@go test -timeout 30m ./...
.PHONY: test

## test-short: Run tests in short mode.
test-short:
	@echo "--> Running tests in short mode"
	@go test ./... -short -timeout 1m
.PHONY: test-short

## test-e2e: Run end to end tests via knuu. This command requires a kube/config file to configure kubernetes.
test-e2e:
	@echo "--> Running end to end tests"
	go run ./test/e2e $(filter-out $@,$(MAKECMDGOALS))
.PHONY: test-e2e

## test-race: Run tests in race mode.
test-race:
# TODO: Remove the -skip flag once the following tests no longer contain data races.
# https://github.com/celestiaorg/celestia-app/issues/1369
	@echo "--> Running tests in race mode"
	@go test ./... -v -race -skip "TestPrepareProposalConsistency|TestIntegrationTestSuite|TestBlobstreamRPCQueries|TestSquareSizeIntegrationTest|TestStandardSDKIntegrationTestSuite|TestTxsimCommandFlags|TestTxsimCommandEnvVar|TestMintIntegrationTestSuite|TestBlobstreamCLI|TestUpgrade|TestMaliciousTestNode|TestBigBlobSuite|TestQGBIntegrationSuite|TestSignerTestSuite|TestPriorityTestSuite|TestTimeInPrepareProposalContext|TestBlobstream|TestCLITestSuite|TestLegacyUpgrade|TestSignerTwins|TestConcurrentTxSubmission|TestTxClientTestSuite|Test_testnode"
.PHONY: test-race

## test-bench: Run unit tests in bench mode.
test-bench:
	@echo "--> Running tests in bench mode"
	@go test -bench=. ./...
.PHONY: test-bench

## test-coverage: Generate test coverage.txt
test-coverage:
	@echo "--> Generating coverage.txt"
	@export VERSION=$(VERSION); bash -x scripts/test_cover.sh
.PHONY: test-coverage

## test-fuzz: Run all fuzz tests.
test-fuzz:
	bash -x scripts/test_fuzz.sh
.PHONY: test-fuzz

## test-interchain: Run interchain tests in verbose mode. Requires Docker.
test-interchain:
	@echo "Reminder: this test uses the latest celestia-app Docker image. If you would like to test recent code changes, re-build the Docker image by running: make build-docker"
	@echo "--> Running interchain tests"
	@cd ./test/interchain && go test . -v
.PHONY: test-interchain

## txsim-install: Install the tx simulator.
txsim-install:
	@echo "--> Installing tx simulator"
	@go install $(BUILD_FLAGS) ./test/cmd/txsim
.PHONY: txsim-install

## txsim-build: Build the tx simulator binary into the ./build directory.
txsim-build:
	@echo "--> Building tx simulator"
	@cd ./test/cmd/txsim
	@mkdir -p build/
	@go build $(BUILD_FLAGS) -o build/ ./test/cmd/txsim
	@go mod tidy
.PHONY: txsim-build

## txsim-build-docker: Build the tx simulator Docker image. Requires Docker.
txsim-build-docker:
	docker build -t ghcr.io/celestiaorg/txsim -f docker/Dockerfile_txsim  .
.PHONY: txsim-build-docker

## adr-gen: Download the ADR template from the celestiaorg/.github repo.
adr-gen:
	@echo "--> Downloading ADR template"
	@curl -sSL https://raw.githubusercontent.com/celestiaorg/.github/main/adr-template.md > docs/architecture/adr-template.md
.PHONY: adr-gen

## prebuilt-binary: Create prebuilt binaries and attach them to GitHub release. Requires Docker.
prebuilt-binary:
	@if [ ! -f ".release-env" ]; then \
		echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope."; \
		exit 1;\
	fi
	docker run \
		--rm \
		-e CGO_ENABLED=1 \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		release --clean
.PHONY: prebuilt-binary
