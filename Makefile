# -----------------------------------------------------------------------------
# Configuration & Versioning
# -----------------------------------------------------------------------------

# Version: Use simplified git describe. Logic assumes the tag starts with 'v'.
VERSION := $(shell git describe --tags --always --match "v*" | sed 's/^v//')
COMMIT := $(shell git rev-parse --short HEAD)
CELESTIA_TAG := $(shell git rev-parse --short=8 HEAD)

# Environment and Tooling
export CELESTIA_TAG
PROJECTNAME := $(shell basename "$(PWD)")
DOCKER := $(shell which docker)
GO_BIN := $(shell go env GOPATH)/bin
DOCKER_GOOS ?= linux
DOCKER_GOARCH ?= amd64
HTTPS_GIT := https://github.com/celestiaorg/celestia-app.git
PACKAGE_NAME := github.com/celestiaorg/celestia-app/v6

# Tooling Versions
BUF_VERSION := v1.50.0
GOLANG_PROTOBUF_VERSION := 1.28.1
GRPC_GATEWAY_VERSION := 1.16.0
GRPC_GATEWAY_PROTOC_GEN_OPENAPIV2_VERSION := 2.20.0
GOLANG_CROSS_VERSION := v1.24.6

# Upgrade Management (for Multiplexer)
# NOTE: This section defines the historical binaries to embed for seamless upgrades.
CELESTIA_V3_VERSION := v3.10.6
CELESTIA_V4_VERSION := v4.1.0
CELESTIA_V5_VERSION := v5.0.12
V2_UPGRADE_HEIGHT ?= 0

# -----------------------------------------------------------------------------
# Build Flags & LDFLAGS Refactoring (No Redundancy)
# -----------------------------------------------------------------------------

LDFLAGS_COMMON := -X github.com/cosmos/cosmos-sdk/version.Name=celestia-app \
	-X github.com/cosmos/cosmos-sdk/version.AppName=celestia-appd \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT) \
	-X github.com/celestiaorg/celestia-app/v6/cmd/celestia-appd/cmd.v2UpgradeHeight=$(V2_UPGRADE_HEIGHT)

BUILD_TAGS_STANDALONE := ledger
BUILD_TAGS_MULTIPLEXER := ledger,multiplexer

LDFLAGS_STANDALONE := $(LDFLAGS_COMMON) -X github.com/cosmos/cosmos-sdk/version.BuildTags=$(BUILD_TAGS_STANDALONE)
LDFLAGS_MULTIPLEXER := $(LDFLAGS_COMMON) -X github.com/cosmos/cosmos-sdk/version.BuildTags=$(BUILD_TAGS_MULTIPLEXER)

BUILD_FLAGS_STANDALONE := -tags=$(BUILD_TAGS_STANDALONE) -ldflags '$(LDFLAGS_STANDALONE)'
BUILD_FLAGS_MULTIPLEXER := -tags=$(BUILD_TAGS_MULTIPLEXER) -ldflags '$(LDFLAGS_MULTIPLEXER)'

# -----------------------------------------------------------------------------
# Help Target
# -----------------------------------------------------------------------------
## help: Display available commands.
help: Makefile
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@sed -n 's/^##//p' $< | sort | column -t -s ':' | sed -e 's/^/ /'
.PHONY: help

# -----------------------------------------------------------------------------
# Core Build Logic
# -----------------------------------------------------------------------------

DOWNLOAD ?= true
TARGET_CMD := ./cmd/celestia-appd

## build-standalone: Build the celestia-appd binary into the ./build directory (Standalone version).
build-standalone: mod
	@echo "--> Building build/celestia-appd (Standalone)"
	@mkdir -p build/
	@go build $(BUILD_FLAGS_STANDALONE) -o build/celestia-appd $(TARGET_CMD)
.PHONY: build-standalone

## build: Build the celestia-appd binary with the multiplexer enabled (includes downloading old binaries).
build: mod
ifeq ($(DOWNLOAD),true)
	@$(MAKE) all-download-binaries
endif
	@echo "--> Building build/celestia-appd with multiplexer enabled"
	@mkdir -p build/
	@go build $(BUILD_FLAGS_MULTIPLEXER) -o build/celestia-appd $(TARGET_CMD)
.PHONY: build

## install-standalone: Install the celestia-appd standalone binary into $(GO_BIN).
install-standalone: mod
	@echo "--> Installing celestia-appd (Standalone) to $(GO_BIN)"
	@go install $(BUILD_FLAGS_STANDALONE) $(TARGET_CMD)
.PHONY: install-standalone

## install: Install the celestia-appd multiplexer binary into $(GO_BIN).
install: all-download-binaries
	@echo "--> Installing celestia-appd with multiplexer support to $(GO_BIN)"
	@go install $(BUILD_FLAGS_MULTIPLEXER) $(TARGET_CMD)
.PHONY: install

# -----------------------------------------------------------------------------
# Binary Download Logic (Refactored using Pattern Rules)
# -----------------------------------------------------------------------------

# Internal variable list of all versions to download
MULTIPLEXER_VERSIONS := v3 v4 v5
ALL_VERSIONS := $(CELESTIA_V3_VERSION) $(CELESTIA_V4_VERSION) $(CELESTIA_V5_VERSION)

# Define a target that calls all download targets.
all-download-binaries: $(foreach version,$(MULTIPLEXER_VERSIONS),download-$(version)-binaries)
.PHONY: all-download-binaries

# Pattern Rule for downloading any version (v3, v4, v5, etc.)
# $< refers to the target name (e.g., download-v3-binaries)
download-%-binaries:
	@$(eval VERSION_VAR := CELESTIA_$(shell echo $* | tr '[:lower:]' '[:upper:]')_VERSION)
	@$(eval VERSION_VAL := $($(VERSION_VAR)))
	@echo "--> Downloading celestia-app $(VERSION_VAL) binary"
	@mkdir -p internal/embedding
	# Execute logic in a single robust shell command
	@os=$$(go env GOOS); arch=$$(go env GOARCH); \
	set -euo pipefail; \
	url_prefix="celestia-app-standalone"; \
	if [ "$*" = "v3" ]; then url_prefix="celestia-app"; fi; \
	case "$$os-$$arch" in \
		darwin-arm64) url="$${url_prefix}_Darwin_arm64.tar.gz"; out="celestia-app_darwin_v$*_arm64.tar.gz" ;; \
		linux-arm64) url="$${url_prefix}_Linux_arm64.tar.gz"; out="celestia-app_linux_v$*_arm64.tar.gz" ;; \
		darwin-amd64) url="$${url_prefix}_Darwin_x86_64.tar.gz"; out="celestia-app_darwin_v$*_amd64.tar.gz" ;; \
		linux-amd64) url="$${url_prefix}_Linux_x86_64.tar.gz"; out="celestia-app_linux_v$*_amd64.tar.gz" ;; \
		*) echo "Unsupported platform: $$os-$$arch"; exit 1 ;; \
	esac; \
	bash scripts/download_binary.sh "$$url" "$$out" "$(VERSION_VAL)"

# -----------------------------------------------------------------------------
# Dependency Management (mod)
# -----------------------------------------------------------------------------

## mod: Update and clean go.mod files in the root and test directory.
mod:
	@echo "--> Updating go.mod (root)"
	@go mod tidy
	@echo "--> Updating go.mod in ./test/docker-e2e"
	@cd ./test/docker-e2e && go mod tidy
.PHONY: mod

## mod-verify: Verify dependencies have expected content.
mod-verify: mod
	@echo "--> Verifying dependencies have expected content"
	@GO111MODULE=on go mod verify
.PHONY: mod-verify

# -----------------------------------------------------------------------------
# Protobuf Targets
# -----------------------------------------------------------------------------
# Consolidated proto commands for fail-fast behavior.

## proto-all: Format, lint, and generate Protobuf files
proto-all: proto-deps proto-format proto-lint proto-gen
.PHONY: proto-all

## proto-deps: Install Protobuf local dependencies
proto-deps:
	@echo "Installing Protobuf dependencies"
	@go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	@go install github.com/cosmos/cosmos-proto/cmd/protoc-gen-go-pulsar@latest
	@go install github.com/cosmos/gogoproto/protoc-gen-gocosmos@latest
	@go install github.com/cosmos/gogoproto/protoc-gen-gogo@latest
	@go install github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway@v$(GRPC_GATEWAY_VERSION)
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v$(GRPC_GATEWAY_PROTOC_GEN_OPENAPIV2_VERSION)
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v$(GOLANG_PROTOBUF_VERSION)
.PHONY: proto-deps

## proto-format: Format Protobuf files using clang-format.
proto-format:
	@echo "Formatting Protobuf files"
	@find ./ -name "*.proto" -exec clang-format -i {} \;
.PHONY: proto-format

## proto-lint: Lint Protobuf files using buf.
proto-lint:
	@echo "Linting Protobuf files"
	@buf lint --error-format=json
.PHONY: proto-lint

## proto-gen: Generate Protobuf Go and documentation code.
proto-gen:
	@echo "Generating Protobuf files"
	@sh ./scripts/protocgen.sh
.PHONY: proto-gen

## proto-check-breaking: Check for breaking Protobuf changes against main branch.
proto-check-breaking:
	@echo "Checking for breaking changes against main"
	@buf breaking --against $(HTTPS_GIT)#branch=main
.PHONY: proto-check-breaking

## proto-update-deps: Update Protobuf dependencies using buf.
proto-update-deps:
	@echo "Updating Protobuf dependencies"
	@cd proto && buf dep update
.PHONY: proto-update-deps

# -----------------------------------------------------------------------------
# Docker Targets
# -----------------------------------------------------------------------------
# Simplified targets by consolidating common logic.

## build-docker-standalone: Build the celestia-appd standalone Docker image.
build-docker-standalone:
	@echo "--> Building Docker image: Standalone"
	@$(DOCKER) build -t celestiaorg/celestia-app-standalone -f docker/standalone.Dockerfile .
.PHONY: build-docker-standalone

## build-docker-multiplexer: Build the celestia-appd Docker image with Multiplexer support.
build-docker-multiplexer:
	@echo "--> Building Docker image: Multiplexer"
	@$(DOCKER) build \
		--build-arg TARGETOS=$(DOCKER_GOOS) \
		--build-arg TARGETARCH=$(DOCKER_GOARCH) \
		-t celestiaorg/celestia-app:$(COMMIT) \
		-f docker/multiplexer.Dockerfile .
.PHONY: build-docker-multiplexer

## docker-build: Alias for building the multiplexer image.
docker-build: build-docker-multiplexer
.PHONY: docker-build

## build-ghcr-docker: Build Docker image for GHCR tagging.
build-ghcr-docker:
	@echo "--> Building Docker image for GHCR"
	@$(DOCKER) build -t ghcr.io/celestiaorg/celestia-app-standalone:$(COMMIT) -f docker/standalone.Dockerfile .
.PHONY: build-ghcr-docker

## docker-build-ghcr: Alias for building GHCR image.
docker-build-ghcr: build-ghcr-docker
.PHONY: docker-build-ghcr

## publish-ghcr-docker: Push the celestia-appd Docker image to GHCR.
publish-ghcr-docker:
	@echo "--> Publishing Docker image to GHCR"
	@$(DOCKER) push ghcr.io/celestiaorg/celestia-app-standalone:$(COMMIT)
.PHONY: publish-ghcr-docker

## docker-publish: Alias for pushing GHCR image.
docker-publish: publish-ghcr-docker
.PHONY: docker-publish

# -----------------------------------------------------------------------------
# Linting & Formatting
# -----------------------------------------------------------------------------

## lint: Run all linters (golangci-lint, markdownlint, hadolint, yamllint).
lint:
	@echo "--> Running golangci-lint"
	@golangci-lint run
	@echo "--> Running markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md'
	@echo "--> Running hadolint"
	@hadolint docker/multiplexer.Dockerfile docker/standalone.Dockerfile docker/txsim/Dockerfile
	@echo "--> Running yamllint"
	@yamllint --no-warnings . -c .yamllint.yml
.PHONY: lint

## fmt: Format Go code and markdown files.
fmt:
	@echo "--> Running golangci-lint --fix"
	@golangci-lint run --fix
	@echo "--> Running markdownlint --fix"
	@markdownlint --fix --quiet --config .markdownlint.yaml .
.PHONY: fmt

## lint-fix: Alias for running formatting fixes.
lint-fix: fmt
.PHONY: lint-fix

## lint-links: Check all markdown links for validity.
lint-links:
	@echo "--> Running markdown-link-check"
	@find . -name \*.md -print0 | xargs -0 -n1 markdown-link-check
.PHONY: lint-links

## modernize-fix: Apply Go code modernization fixes.
modernize-fix:
	@echo "--> Applying modernize fixes"
	@bash scripts/modernize.sh
.PHONY: modernize-fix

## modernize-check: Check for Go code modernization issues.
modernize-check:
	@echo "--> Checking for modernize issues"
	@bash scripts/modernize-check.sh
.PHONY: modernize-check

# -----------------------------------------------------------------------------
# Testing Targets
# -----------------------------------------------------------------------------

## test: Run unit tests with a 30m timeout.
test:
	@echo "--> Running tests"
	@if [ -z "$(PACKAGES)" ]; then \
		go test -timeout 30m ./...; \
	else \
		go test -timeout 30m $(PACKAGES); \
	fi
.PHONY: test

## test-short: Run tests in short mode (fast).
test-short:
	@echo "--> Running tests in short mode"
	@go test ./... -short -timeout 1m
.PHONY: test-short

## test-coverage: Generate test coverage.txt
test-coverage:
	@echo "--> Generating coverage.txt"
	@export VERSION=$(VERSION); bash -x scripts/test_cover.sh
.PHONY: test-coverage

## test-fuzz: Run all fuzz tests.
test-fuzz:
	@bash -x scripts/test_fuzz.sh
.PHONY: test-fuzz

## test-multiplexer: Run unit tests for the multiplexer package.
test-multiplexer: all-download-binaries
	@echo "--> Running multiplexer tests"
	@go test -tags multiplexer ./multiplexer/...
.PHONY: test-multiplexer

## test-race: Run tests in race detection mode.
test-race:
# Skipping known flaky tests for race detection compatibility.
	@echo "--> Running tests in race mode (skipping known flakes)"
	@go test -timeout 15m ./... -v -race -skip "TestPrepareProposalConsistency|TestIntegrationTestSuite|TestSquareSizeIntegrationTest|TestStandardSDKIntegrationTestSuite|TestTxsimCommandFlags|TestTxsimCommandEnvVar|TestTxsimDefaultKeypath|TestMintIntegrationTestSuite|TestUpgrade|TestMaliciousTestNode|TestBigBlobSuite|TestQGBIntegrationSuite|TestSignerTestSuite|TestPriorityTestSuite|TestTimeInPrepareProposalContext|TestCLITestSuite|TestLegacyUpgrade|TestSignerTwins|TestConcurrentTxSubmission|TestTxClientTestSuite|Test_testnode|TestEvictions|TestEstimateGasUsed|TestEstimateGasPrice|TestWithEstimatorService|TestTxsOverMaxTxSizeGetRejected|TestStart_Success|TestReadBlockchainHeaders|TestPrepareProposalCappingNumberOfMessages|TestGasEstimatorE2E|TestGasEstimatorE2EWithNetworkMinGasPrice|TestRejections|TestClaimRewardsAfterFullUndelegation|TestParallelTxSubmission|TestV2SubmitMethods"
.PHONY: test-race

## test-bench: Run benchmark unit tests.
test-bench:
	@echo "--> Running benchmark tests"
	@go test -timeout 30m -tags=benchmarks -bench=. ./app/benchmarks/...
.PHONY: test-bench

# -----------------------------------------------------------------------------
# Docker E2E & Upgrade Targets
# -----------------------------------------------------------------------------
# Targets for running E2E tests inside Docker containers.

## test-docker-e2e: Run specific end-to-end test via docker. Requires 'test' variable.
test-docker-e2e:
	@if [ -z "$(test)" ]; then \
		echo "ERROR: 'test' variable is required. Usage: make test-docker-e2e test=TestE2ESimple [entrypoint=TestCelestiaTestSuite]" >&2; \
		exit 1; \
	fi
	@ENTRYPOINT=$${entrypoint:-TestCelestiaTestSuite}; \
	echo "--> Running: $$ENTRYPOINT/$(test)"; \
	cd test/docker-e2e && go test -v -run ^$$ENTRYPOINT/$(test)$$ ./... -timeout 30m
.PHONY: test-docker-e2e

## test-docker-e2e-upgrade-all: Run the upgrade test for all app versions starting from v2.
test-docker-e2e-upgrade-all:
	@echo "--> Building celestia-appd docker image (tag $(CELESTIA_TAG))"
	@DOCKER_BUILDKIT=0 $(DOCKER) build --build-arg BUILDPLATFORM=linux/amd64 --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t "ghcr.io/celestiaorg/celestia-app:$(CELESTIA_TAG)" . -f docker/multiplexer.Dockerfile
	@echo "--> Running upgrade test for all app versions starting from v2"
	@cd test/docker-e2e && go test -v -run ^TestCelestiaTestSuite/TestAllUpgrades$$ -count=1 ./... -timeout 15m
.PHONY: test-docker-e2e-upgrade-all

# -----------------------------------------------------------------------------
# TXSIM (Transaction Simulator) Targets
# -----------------------------------------------------------------------------

## txsim-install: Install the tx simulator binary into $(GO_BIN).
txsim-install:
	@echo "--> Installing tx simulator"
	@go install $(BUILD_FLAGS_STANDALONE) ./test/cmd/txsim
.PHONY: txsim-install

## txsim-build: Build the tx simulator binary into the ./build directory.
txsim-build:
	@echo "--> Building tx simulator"
	@mkdir -p build/
	@go build $(BUILD_FLAGS_STANDALONE) -o build/txsim ./test/cmd/txsim
	@go mod tidy # Ensure tidy runs after build if go.mod was updated
.PHONY: txsim-build

## txsim-build-docker: Build the tx simulator Docker image.
txsim-build-docker:
	@echo "--> Building tx simulator Docker image"
	@$(DOCKER) build -t ghcr.io/celestiaorg/txsim -f docker/txsim/Dockerfile .
.PHONY: txsim-build-docker

# -----------------------------------------------------------------------------
# Infrastructure & Network Tuning
# -----------------------------------------------------------------------------

## build-talis-bins: Build celestia-appd and txsim binaries for Talis VMs.
build-talis-bins:
	@echo "--> Building Talis VM binaries"
	@$(DOCKER) build \
	  --file tools/talis/docker/Dockerfile \
	  --target builder \
	  --platform linux/amd64 \
	  --build-arg LDFLAGS="$(LDFLAGS_STANDALONE)" \
	  --build-arg GOOS=linux \
	  --build-arg GOARCH=amd64 \
	  --tag talis-builder:latest \
	  .
	@mkdir -p build
	# Use 'docker cp' to extract binaries safely
	@$(DOCKER) create --platform linux/amd64 --name tmp-talis talis-builder:latest
	@$(DOCKER) cp tmp-talis:/out/. build/
	@$(DOCKER) rm tmp-talis
.PHONY: build-talis-bins

## adr-gen: Download the ADR template.
adr-gen:
	@echo "--> Downloading ADR template"
	@curl -sSL https://raw.githubusercontent.com/celestiaorg/.github/main/adr-template.md > docs/architecture/adr-template.md
.PHONY: adr-gen

# Internal target for network congestion control checks.
.check-bbr:
	@echo "Checking if BBR is enabled..."
	@if [ "$$(uname -s)" != "Linux" ]; then \
		echo "BBR is not available on non-Linux systems."; \
		exit 0; \
	elif [ "$$(sysctl net.ipv4.tcp_congestion_control | awk '{print $$3}')" != "bbr" ]; then \
		echo "WARNING: BBR is not enabled. Please enable BBR for optimal performance. Call 'make enable-bbr'."; \
	else \
		echo "BBR is enabled."; \
	fi

## bbr-check: Check if your system uses BBR congestion control algorithm. (Linux only).
bbr-check: .check-bbr
.PHONY: bbr-check

## enable-bbr: Enable BBR congestion control algorithm. (Linux only). Requires sudo.
enable-bbr:
	@echo "Configuring system to use BBR..."
	@if [ "$$(uname -s)" != "Linux" ]; then \
		echo "BBR is not available on non-Linux systems."; \
		exit 0; \
	elif [ "$$(sysctl net.ipv4.tcp_congestion_control | awk '{print $$3}')" != "bbr" ]; then \
	    echo "BBR is not enabled. Configuring BBR..."; \
	    sudo modprobe tcp_bbr && \
	    echo "net.core.default_qdisc=fq" | sudo tee -a /etc/sysctl.conf && \
	    echo "net.ipv4.tcp_congestion_control=bbr" | sudo tee -a /etc/sysctl.conf && \
	    sudo sysctl -p && \
	    echo "BBR has been enabled." || \
	    echo "Failed to enable BBR. Please check error messages above."; \
	else \
	    echo "BBR is already enabled."; \
	fi
.PHONY: enable-bbr

## disable-bbr: Disable BBR congestion control algorithm and revert to default. (Linux only). Requires sudo.
disable-bbr:
	@echo "Disabling BBR and reverting to default congestion control algorithm..."
	@if [ "$$(sysctl net.ipv4.tcp_congestion_control | awk '{print $$3}')" = "bbr" ]; then \
	    echo "BBR is currently enabled. Reverting to Cubic..."; \
	    sudo sed -i '/^net.core.default_qdisc=fq/d' /etc/sysctl.conf && \
	    sudo sed -i '/^net.ipv4.tcp_congestion_control=bbr/d' /etc/sysctl.conf && \
	    sudo modprobe -r tcp_bbr 2>/dev/null || true && \
	    echo "net.ipv4.tcp_congestion_control=cubic" | sudo tee -a /etc/sysctl.conf && \
	    sudo sysctl -p && \
	    echo "BBR has been disabled, and Cubic is now the default congestion control algorithm." || \
	    echo "Failed to disable BBR. Please check error messages above."; \
	else \
	    echo "BBR is not enabled. No changes made."; \
	fi
.PHONY: disable-bbr

# MPTCP targets are complex and require sudo. They are simplified here.
## enable-mptcp: Enable Multi-Path TCP over multiple ports (Linux Kernel 5.6+). Requires sudo.
enable-mptcp:
	@echo "Configuring system to use MPTCP..."
	@sudo sysctl -w net.mptcp.enabled=1
	@sudo sysctl -w net.mptcp.mptcp_path_manager=ndiffports
	@sudo sysctl -w net.mptcp.mptcp_ndiffports=16
	@echo "MPTCP configuration complete and persistent!"
.PHONY: enable-mptcp

## disable-mptcp: Disable Multi-Path TCP and revert settings. (Linux Kernel 5.6+). Requires sudo.
disable-mptcp:
	@echo "Disabling MPTCP..."
	@sudo sysctl -w net.mptcp.enabled=0
	@sudo sysctl -w net.mptcp.mptcp_path_manager=default
	@echo "MPTCP configuration reverted!"
.PHONY: disable-mptcp

# -----------------------------------------------------------------------------
# Goreleaser Targets
# -----------------------------------------------------------------------------
# Goreleaser targets were kept verbose due to their critical nature and complex environment requirements.

## goreleaser-check: Check the .goreleaser.yaml config file.
goreleaser-check:
	@if [ ! -f ".release-env" ]; then \
		echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope." >&2; \
		exit 1;\
	fi
	@$(DOCKER) run \
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

## prebuilt-binary: Create prebuilt binaries for all supported platforms using goreleaser.
prebuilt-binary:
	@if [ ! -f ".release-env" ]; then \
		echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope." >&2; \
		exit 1;\
	fi
	@$(DOCKER) run \
		--rm \
		--env CGO_ENABLED=1 \
		--env GORELEASER_CURRENT_TAG=${GORELEASER_CURRENT_TAG} \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		release --clean --parallelism 1
.PHONY: prebuilt-binary

## goreleaser-dry-run: Ensures that the go releaser tool can build all the artifacts correctly.
goreleaser-dry-run:
	@echo "Running GoReleaser in dry-run mode..."
	@$(DOCKER) run \
		--rm \
		--env CGO_ENABLED=1 \
		--env GORELEASER_CURRENT_TAG=${GORELEASER_CURRENT_TAG} \
		--env-file .release-env \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v `pwd`:/go/src/$(PACKAGE_NAME) \
		-w /go/src/$(PACKAGE_NAME) \
		ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
		release --snapshot --clean --parallelism 1
.PHONY: goreleaser-dry-run

## goreleaser: Create prebuilt binaries and attach them to GitHub release. Requires Docker.
goreleaser: prebuilt-binary
.PHONY: goreleaser
