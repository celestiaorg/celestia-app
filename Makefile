PROJECTNAME=$(shell basename "$(PWD)")

## help: Get more info on make commands.
help: Makefile
	@echo " Choose a command run in "$(PROJECTNAME)":"
	@sed -n 's/^##//p' $< | sort | column -t -s ':' | sed -e 's/^/ /'
.PHONY: help

## test: Run unit tests.
test:
	@echo "--> Running unit tests"
	@go test
.PHONY: test

## bench: Run benchmarks.
bench:
	@echo "--> Running benchmarks"
	@go test -benchmem -bench=.
.PHONY: bench

## lint: Run golangci-lint and markdownlint.
lint:
	@echo "--> Running golangci-lint"
	@golangci-lint run
	@echo "--> Running markdownlint"
	@markdownlint --config .markdownlint.yaml '**/*.md'
.PHONY: lint
