VERSION := $(shell echo $(shell git describe --tags 2>/dev/null || git log -1 --format='%h') | sed 's/^v//')
COMMIT := $(shell git rev-parse --short HEAD)
DOCKER := $(shell which docker)
PROJECTNAME=$(shell basename "$(PWD)")
HTTPS_GIT := https://github.com/celestiaorg/celestia-app.git
PACKAGE_NAME          := github.com/celestiaorg/celestia-app/v4
# Before upgrading the GOLANG_CROSS_VERSION, please verify that a Docker image exists with the new tag.
# See https://github.com/goreleaser/goreleaser-cross/pkgs/container/goreleaser-cross
GOLANG_CROSS_VERSION  ?= v1.23.6
# Set this to override the max square size of the binary
OVERRIDE_MAX_SQUARE_SIZE ?=

# process linker flags
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=celestia-app \
		  -X github.com/cosmos/cosmos-sdk/version.AppName=celestia-appd \
		  -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
		  -X github.com/celestiaorg/celestia-app/v4/pkg/appconsts.OverrideSquareSizeUpperBoundStr=$(OVERRIDE_MAX_SQUARE_SIZE)

BUILD_FLAGS := -tags "ledger" -ldflags '$(ldflags)'

## help: Get more info on make commands.
help: Makefile
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@sed -n 's/^##//p' $< | sort | column -t -s ':' | sed -e 's/^/ /'
.PHONY: help

## build: Build the celestia-appd binary into the ./build directory.
build: mod
	@cd ./cmd/celestia-appd
	@mkdir -p build/
	@echo "--> Building build/celestia-appd"
	@go build $(BUILD_FLAGS) -o build/ ./cmd/celestia-appd
.PHONY: build

## install: Build and install the celestia-appd binary into the $GOPATH/bin directory.
install: check-bbr
	@echo "--> Installing celestia-appd"
	@go install $(BUILD_FLAGS) ./cmd/celestia-appd
.PHONY: install

## mod: Update all go.mod files.
mod:
	@echo "--> Updating go.mod"
	@go mod tidy
	@echo "--> Updating go.mod in ./test/interchain"
	@(cd ./test/interchain && go mod tidy)
.PHONY: mod

## mod-verify: Verify dependencies have expected content.
mod-verify: mod
	@echo "--> Verifying dependencies have expected content"
	GO111MODULE=on go mod verify
.PHONY: mod-verify

BUF_VERSION=v1.50.0
GOLANG_PROTOBUF_VERSION=1.28.1
GRPC_GATEWAY_VERSION=1.16.0
GRPC_GATEWAY_PROTOC_GEN_OPENAPIV2_VERSION=2.20.0

## proto-all: Format, lint and generate Protobuf files
proto-all: proto-deps proto-format proto-lint proto-gen

## proto-deps: Install Protobuf local dependencies
proto-deps:
	@echo "Installing proto deps"
	@go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	@go install github.com/cosmos/cosmos-proto/cmd/protoc-gen-go-pulsar@latest
	@go install github.com/cosmos/gogoproto/protoc-gen-gocosmos@latest
	@go install github.com/cosmos/gogoproto/protoc-gen-gogo@latest
	@go install github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway@v$(GRPC_GATEWAY_VERSION)
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v$(GRPC_GATEWAY_PROTOC_GEN_OPENAPIV2_VERSION)
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v$(GOLANG_PROTOBUF_VERSION)

## proto-gen: Generate Protobuf files.
proto-gen:
	@echo "Generating Protobuf files"
	@sh ./scripts/protocgen.sh

## proto-format: Format Protobuf files.
proto-format:
	@find ./ -name "*.proto" -exec clang-format -i {} \;

## proto-lint: Lint Protobuf files.
proto-lint:
	@buf lint --error-format=json

## proto-check-breaking: Check if Protobuf file contains breaking changes.
proto-check-breaking:
	@buf breaking --against $(HTTPS_GIT)#branch=main

## proto-update-deps: Update Protobuf dependencies.
proto-update-deps:
	@echo "Updating Protobuf dependencies"
	@cd proto && buf dep update

.PHONY: proto-all proto-deps proto-gen proto-format proto-lint proto-check-breaking proto-update-deps

build-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t celestiaorg/celestia-app -f docker/Dockerfile .
.PHONY: build-docker

## docker-build: Build the celestia-appd docker image from the current branch. Requires docker.
docker-build: build-docker
.PHONY: docker-build

build-ghcr-docker:
	@echo "--> Building Docker image"
	$(DOCKER) build -t ghcr.io/celestiaorg/celestia-app:$(COMMIT) -f docker/Dockerfile .
.PHONY: build-ghcr-docker

## docker-build-ghcr: Build the celestia-appd docker image from the last commit. Requires docker.
docker-build-ghcr: build-ghcr-docker
.PHONY: docker-build-ghcr

publish-ghcr-docker:
# Make sure you are logged in and authenticated to the ghcr.io registry.
	@echo "--> Publishing Docker image"
	$(DOCKER) push ghcr.io/celestiaorg/celestia-app:$(COMMIT)
.PHONY: publish-ghcr-docker

## docker-publish: Publish the celestia-appd docker image. Requires docker.
docker-publish: publish-ghcr-docker
.PHONY: docker-publish

## lint: Run all linters; golangci-lint, markdownlint, hadolint, yamllint.
lint:
	@echo "--> Running golangci-lint"
	@golangci-lint run
	@echo "--> Running markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md'
	@echo "--> Running hadolint"
	@hadolint docker/Dockerfile
	@hadolint docker/txsim/Dockerfile
	@echo "--> Running yamllint"
	@yamllint --no-warnings . -c .yamllint.yml
.PHONY: lint

markdown-link-check:
	@echo "--> Running markdown-link-check"
	@find . -name \*.md -print0 | xargs -0 -n1 markdown-link-check
.PHONY: markdown-link-check

## lint-links: Check all markdown links.
lint-links: markdown-link-check
.PHONY: lint-links


fmt:
	@echo "--> Running golangci-lint --fix"
	@golangci-lint run --fix
	@echo "--> Running markdownlint --fix"
	@markdownlint --fix --quiet --config .markdownlint.yaml .
.PHONY: fmt

## lint-fix: Format files per linters golangci-lint and markdownlint.
lint-fix: fmt
.PHONY: lint-fix

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

test-multi-plexer:
	@echo "--> Running multi-plexer tests"
	go test -tags nova -v ./test/nova/...
.PHONY: test-multi-plexer

## test-race: Run tests in race mode.
test-race:
# TODO: Remove the -skip flag once the following tests no longer contain data races.
# https://github.com/celestiaorg/celestia-app/issues/1369
	@echo "--> Running tests in race mode"
	@go test -timeout 15m ./... -v -race -skip "TestPrepareProposalConsistency|TestIntegrationTestSuite|TestSquareSizeIntegrationTest|TestStandardSDKIntegrationTestSuite|TestTxsimCommandFlags|TestTxsimCommandEnvVar|TestTxsimDefaultKeypath|TestMintIntegrationTestSuite|TestUpgrade|TestMaliciousTestNode|TestBigBlobSuite|TestQGBIntegrationSuite|TestSignerTestSuite|TestPriorityTestSuite|TestTimeInPrepareProposalContext|TestCLITestSuite|TestLegacyUpgrade|TestSignerTwins|TestConcurrentTxSubmission|TestTxClientTestSuite|Test_testnode|TestEvictions|TestEstimateGasUsed|TestEstimateGasPrice"
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
	docker build -t ghcr.io/celestiaorg/txsim -f docker/txsim/Dockerfile  .
.PHONY: txsim-build-docker

## adr-gen: Download the ADR template from the celestiaorg/.github repo.
adr-gen:
	@echo "--> Downloading ADR template"
	@curl -sSL https://raw.githubusercontent.com/celestiaorg/.github/main/adr-template.md > docs/architecture/adr-template.md
.PHONY: adr-gen

## goreleaser-check: Check the .goreleaser.yaml config file.
goreleaser-check:
	@if [ ! -f ".release-env" ]; then \
		echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope."; \
		exit 1;\
	fi
	docker run \
		--rm \
		--env CGO_ENABLED=1 \
		--env GORELEASER_CURRENT_TAG=${GORELEASER_CURRENT_TAG} \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		check
.PHONY: goreleaser-check

prebuilt-binary:
	@if [ ! -f ".release-env" ]; then \
		echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope."; \
		exit 1;\
	fi
	docker run \
		--rm \
		--env CGO_ENABLED=1 \
		--env GORELEASER_CURRENT_TAG=${GORELEASER_CURRENT_TAG} \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		release --clean
.PHONY: prebuilt-binary

## goreleaser: Create prebuilt binaries and attach them to GitHub release. Requires Docker.
goreleaser: prebuilt-binary
.PHONY: goreleaser

check-bbr:
	@echo "Checking if BBR is enabled..."
	@if [ "$$(sysctl net.ipv4.tcp_congestion_control | awk '{print $$3}')" != "bbr" ]; then \
		echo "WARNING: BBR is not enabled. Please enable BBR for optimal performance. Call make enable-bbr or see Usage section in the README."; \
	else \
		echo "BBR is enabled."; \
	fi
.PHONY: check-bbr

## bbr-check: Check if your system uses BBR congestion control algorithm. Only works on Linux.
bbr-check: check-bbr
.PHONY: bbr-check

enable-bbr:
	@echo "Configuring system to use BBR..."
	@if [ "$(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}')" != "bbr" ]; then \
	    echo "BBR is not enabled. Configuring BBR..."; \
	    sudo modprobe tcp_bbr; \
            echo tcp_bbr | sudo tee -a /etc/modules; \
	    echo "net.core.default_qdisc=fq" | sudo tee -a /etc/sysctl.conf; \
	    echo "net.ipv4.tcp_congestion_control=bbr" | sudo tee -a /etc/sysctl.conf; \
	    sudo sysctl -p; \
	    echo "BBR has been enabled."; \
	else \
	    echo "BBR is already enabled."; \
	fi
.PHONY: enable-bbr

## bbr-enable: Enable BBR congestion control algorithm. Only works on Linux.
bbr-enable: enable-bbr
.PHONY: bbr-enable

disable-bbr:
	@echo "Disabling BBR and reverting to default congestion control algorithm..."
	@if [ "$$(sysctl net.ipv4.tcp_congestion_control | awk '{print $$3}')" = "bbr" ]; then \
	    echo "BBR is currently enabled. Reverting to Cubic..."; \
	    sudo sed -i '/^net.core.default_qdisc=fq/d' /etc/sysctl.conf; \
	    sudo sed -i '/^net.ipv4.tcp_congestion_control=bbr/d' /etc/sysctl.conf; \
	    sudo modprobe -r tcp_bbr; \
	    echo "net.ipv4.tcp_congestion_control=cubic" | sudo tee -a /etc/sysctl.conf; \
	    sudo sysctl -p; \
	    echo "BBR has been disabled, and Cubic is now the default congestion control algorithm."; \
	else \
	    echo "BBR is not enabled. No changes made."; \
	fi
.PHONY: disable-bbr

## bbr-disable: Disable BBR congestion control algorithm and revert to default.
bbr-disable: disable-bbr
.PHONY: bbr-disable

enable-mptcp:
	@echo "Configuring system to use mptcp..."
	@sudo sysctl -w net.mptcp.enabled=1
	@sudo sysctl -w net.mptcp.mptcp_path_manager=ndiffports
	@sudo sysctl -w net.mptcp.mptcp_ndiffports=16
	@echo "Making MPTCP settings persistent across reboots..."
	@echo "net.mptcp.enabled=1" | sudo tee -a /etc/sysctl.conf
	@echo "net.mptcp.mptcp_path_manager=ndiffports" | sudo tee -a /etc/sysctl.conf
	@echo "net.mptcp.mptcp_ndiffports=16" | sudo tee -a /etc/sysctl.conf
	@echo "MPTCP configuration complete and persistent!"
.PHONY: enable-mptcp

## mptcp-enable: Enable mptcp over multiple ports (not interfaces). Only works on Linux Kernel 5.6 and above.
mptcp-enable: enable-mptcp
.PHONY: mptcp-enable

disable-mptcp:
	@echo "Disabling MPTCP..."
	@sudo sysctl -w net.mptcp.enabled=0
	@sudo sysctl -w net.mptcp.mptcp_path_manager=default
	@echo "Removing MPTCP settings from /etc/sysctl.conf..."
	@sudo sed -i '/net.mptcp.enabled=1/d' /etc/sysctl.conf
	@sudo sed -i '/net.mptcp.mptcp_path_manager=ndiffports/d' /etc/sysctl.conf
	@sudo sed -i '/net.mptcp.mptcp_ndiffports=16/d' /etc/sysctl.conf
	@echo "MPTCP configuration reverted!"
.PHONY: disable-mptcp

## mptcp-disable: Disable mptcp over multiple ports. Only works on Linux Kernel 5.6 and above.
mptcp-disable: disable-mptcp

CONFIG_FILE ?= ${HOME}/.celestia-app/config/config.toml
SEND_RECV_RATE ?= 10485760  # 10 MiB
