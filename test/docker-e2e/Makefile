## test-e2e: Run end to end tests via docker.
test-e2e:
	@if [ -z "$(test)" ]; then \
		echo "ERROR: 'test' variable is required. Usage: make test-e2e test=TestE2ESimple"; \
		exit 1; \
	fi
	@echo "--> Running: TestCelestiaTestSuite/$(test)"
	go test -v -run ^TestCelestiaTestSuite/$(test)$$ ./...
