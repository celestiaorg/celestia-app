# Define Go command
GO ?= go

# Run unit tests
test:
	$(GO) test ./... -v

test-short:
	$(GO) test ./... -v -short

# lint code with golangci-lint
lint-fix:
	golangci-lint run --fix

.PHONY: test test-short lint-fix
