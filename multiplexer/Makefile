# Define Go command
GO ?= go

# Run unit tests
test:
	$(GO) test ./... -v

test-short:
	$(GO) test ./... -v -short

# Run tests with coverage
test-cover:
	$(GO) test ./... -cover -v

# lint code with golangci-lint
lint-fix:
	golangci-lint run --fix

.PHONY: test test-cover test-short lint-fix
