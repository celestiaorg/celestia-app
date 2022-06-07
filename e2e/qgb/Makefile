#!/usr/bin/make -f

PACKAGES=$(shell go list ./...)

all: test

test:
	@QGB_INTEGRATION_TEST=true go test -mod=readonly -test.timeout 30m -v $(PACKAGES)
