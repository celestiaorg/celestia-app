PACKAGES=$(shell go list ./... | grep -v '/simulation')

VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')

# ldflags = -X github.com/lazyledger/cosmos-sdk/version.Name=lazyledger-app \
# 	-X github.com/lazyledger/cosmos-sdk/version.ServerName=lazyledger-appd \
# 	-X github.com/lazyledger/cosmos-sdk/version.Version=$(VERSION) \
# 	-X github.com/lazyledger/cosmos-sdk/version.Commit=$(COMMIT) 

# BUILD_FLAGS := -ldflags '$(ldflags)'

all: install

install: go.sum
		@echo "--> Installing lazyledger-appd"
		@go install -mod=readonly ./cmd/lazyledger-appd

go.sum: go.mod
		@echo "--> Ensure dependencies have not been modified"
		GO111MODULE=on go mod verify

test:
	@go test -mod=readonly $(PACKAGES)
