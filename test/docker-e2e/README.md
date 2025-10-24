# Docker E2E Tests for Celestia App

This directory contains end-to-end tests for Celestia App using Docker containers and the [Tastora framework](https://github.com/celestiaorg/tastora). These tests set up local blockchain networks to test various scenarios, making them ideal for testing modifications, bug bounties, and integration testing.

## Prerequisites

- **Docker**: Make sure Docker is installed and running
  ```bash
  docker --version
  docker system info
  ```

- **Go**: Go 1.24+ is required
  ```bash
  go version
  ```

- **Make**: Build tools
  ```bash
  make --version
  ```

## Overview

The docker-e2e tests use the Tastora framework to:
- Spin up containerized Celestia blockchain networks
- Configure multiple validators
- Simulate transaction loads with txsim
- Test various scenarios like state sync, upgrades, and IBC

### Test Structure

```
test/docker-e2e/
├── README.md                     # This file
├── dockerchain/                  # Docker chain configuration utilities
│   ├── config.go                # Configuration management
│   └── testchain.go             # Chain setup and utilities
├── networks/                    # Network configurations
├── e2e_*_test.go               # Individual test files
├── go.mod                      # Go module for docker-e2e tests
└── go.sum                      # Go module checksums
```

## Quick Start

### 1. Using Pre-built Images (Recommended for testing existing versions)

The simplest way to run tests is using published Celestia App images. **Important**: Due to how the Makefile sets environment variables, you should run tests directly with `go test` rather than through `make`:

```bash
# Navigate to the docker-e2e directory
cd /path/to/celestia-app/test/docker-e2e

# Run a simple test with the default image (v5.0.10)
go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m

# Use a specific published version
CELESTIA_TAG=v5.0.10 go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m

# Use a different image registry
CELESTIA_IMAGE=ghcr.io/celestiaorg/celestia-app-standalone CELESTIA_TAG=v4.1.0 go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m
```

**Note**: The Makefile exports `CELESTIA_TAG` as the current git commit hash, which overrides environment variables. Therefore, it's recommended to use `go test` directly for more control.

**Alternative with Makefile**: If you prefer using the Makefile, you must first build a local Docker image:
```bash
# Build image with current commit tag
make build-docker-multiplexer
# Then run tests (uses the built image automatically)
make test-docker-e2e test=TestE2ESimple
```

### 2. Using Custom Built Images (For testing local changes)

If you've made changes to the code and want to test them:

```bash
# First, build a Docker image with your changes from the main directory
cd /path/to/celestia-app
make build-docker-multiplexer

# The image will be tagged with your git commit hash
# Check what tag was created:
docker images | grep celestia-app

# Navigate to docker-e2e directory and run tests using your built image
cd test/docker-e2e
# Replace 8eceff78 with your actual commit hash
CELESTIA_TAG=8eceff78 go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m
```

## Testing with Custom celestia-core

If you need to test changes to celestia-core (the consensus layer), follow these steps:

### 1. Modify go.mod to Use Custom celestia-core

Edit the main `go.mod` file to point to your custom celestia-core:

```go
// In go.mod, modify the existing replace directive:
replace github.com/cometbft/cometbft => github.com/yourusername/celestia-core v0.39.10-your-changes

// Or use a local path for development:
replace github.com/cometbft/cometbft => /path/to/your/celestia-core v0.39.10
```

The docker-e2e tests use the same go.mod as the main project, so they will automatically use your custom celestia-core. Just run:
```bash
# Update dependencies after modifying go.mod
make mod
```

### 2. Build Custom Docker Image

```bash
# Update dependencies
make mod

# Build the Docker image with your custom celestia-core
make build-docker-multiplexer

# The image will be tagged with your current git commit
CUSTOM_TAG=$(git rev-parse --short=8 HEAD)
echo "Built image with tag: $CUSTOM_TAG"
```

### 3. Run Tests with Custom Image

```bash
# Navigate to docker-e2e directory and run tests with your custom-built image
cd test/docker-e2e
CELESTIA_TAG=$CUSTOM_TAG go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m
```

## Available Tests

Run individual tests by specifying the test name (from the `test/docker-e2e` directory):

```bash
# Simple functionality test
go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m

# Pay-for-blob (PFB) functionality
go test -v -run ^TestCelestiaTestSuite/TestE2EFullStackPFB$ -timeout 15m

# State sync testing
go test -v -run ^TestCelestiaTestSuite/TestE2EStateSync$ -timeout 15m

# Upgrade testing
go test -v -run ^TestCelestiaTestSuite/TestE2EUpgrade$ -timeout 15m

# IBC (Inter-Blockchain Communication) testing
go test -v -run ^TestCelestiaTestSuite/TestE2EIBC$ -timeout 15m

# Block synchronization
go test -v -run ^TestCelestiaTestSuite/TestE2EBlockSync$ -timeout 15m

# Minor version compatibility
go test -v -run ^TestCelestiaTestSuite/TestE2EMinorVersionCompatibility$ -timeout 15m
```

List all available tests:
```bash
cd test/docker-e2e
go test -list .
```

Run all tests (this will take a while):
```bash
cd test/docker-e2e
go test ./... -timeout 30m
```

## Environment Variables

Control test behavior with these environment variables:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `CELESTIA_TAG` | Docker image tag to use | `v5.0.10` | `v4.1.0`, `main`, `8eceff78` |
| `CELESTIA_IMAGE` | Docker image repository | `ghcr.io/celestiaorg/celestia-app` | `ghcr.io/celestiaorg/celestia-app-standalone` |

## Docker Image Management

### Available Images

Celestia App provides several pre-built images:

- **`ghcr.io/celestiaorg/celestia-app`**: Multiplexer version (includes multiple app versions)
- **`ghcr.io/celestiaorg/celestia-app-standalone`**: Standalone version (single app version)

### Building Your Own Images

For local development and testing custom changes:

```bash
# Build multiplexer version (recommended for testing)
make build-docker-multiplexer

# Build standalone version  
make build-docker-standalone

# Build for GitHub Container Registry
make build-ghcr-docker
```

**Note**: Building Docker images requires internet access to download dependencies. If you encounter network issues during builds, ensure your Docker daemon has proper internet connectivity.

### Troubleshooting Docker Images

If you encounter image pull errors:

1. **Check if image exists**:
   ```bash
   docker pull ghcr.io/celestiaorg/celestia-app:v5.0.10
   ```

2. **Use local build**:
   ```bash
   make build-docker-multiplexer
   cd test/docker-e2e
   CELESTIA_TAG=$(git rev-parse --short=8 HEAD) go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m
   ```

3. **Check available tags**:
   Visit [GitHub Container Registry](https://github.com/orgs/celestiaorg/packages?repo_name=celestia-app) to see available tags.

## Advanced Configuration

### Custom Network Configurations

The tests use configuration in `dockerchain/config.go`. Key configurations:

- **Chain ID**: Default is "test"  
- **Validators**: Default creates 2 validators (validator1, validator2)
- **Gas prices**: Default is "0.025utia"
- **Block time**: 2 seconds

### Transaction Simulation

Tests can optionally run `txsim` to generate transaction load:

```go
// In test code
s.CreateTxSim(ctx, celestia)
```

This simulates realistic transaction patterns including:
- Blob transactions (Pay-for-Blob)
- Bank sends
- Various gas prices and blob sizes

### Custom Test Configuration

Create custom configurations by modifying the default config:

```go
cfg := dockerchain.DefaultConfig(s.client, s.network)
cfg.Image = "ghcr.io/celestiaorg/celestia-app-standalone"
cfg.Tag = "v4.1.0"
// ... other modifications

celestia, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
```

## Common Use Cases

### Bug Bounty Testing

Testing a small security fix:

1. Make your code changes
2. Build local image: `make build-docker-multiplexer`
3. Navigate to test directory: `cd test/docker-e2e`
4. Run specific test: `CELESTIA_TAG=$(git rev-parse --short=8 HEAD) go test -v -run ^TestCelestiaTestSuite/TestE2ESimple$ -timeout 15m`

### Integration Testing

Testing integration with external services:

1. Modify IBC test for your specific chain
2. Update `e2e_ibc_test.go` with your chain configuration
3. Run: `cd test/docker-e2e && go test -v -run ^TestCelestiaTestSuite/TestE2EIBC$ -timeout 15m`

### Performance Testing

Testing performance improvements:

1. Build your improved version
2. Run tests with transaction simulation enabled
3. Compare block times and transaction throughput

## Troubleshooting

### Common Issues

1. **Docker image not found**:
   ```
   Error: manifest unknown
   ```
   **Solution**: Build the image locally or use a valid published tag.

2. **Port conflicts**:
   ```
   Error: port already in use
   ```
   **Solution**: Stop other Celestia processes or use different ports.

3. **Out of disk space**:
   ```
   Error: no space left on device
   ```
   **Solution**: Clean up Docker: `docker system prune -af`

### Debugging Tests

Enable verbose logging:
```bash
cd test/docker-e2e
go test -v -run TestE2ESimple ./... -timeout 10m
```

Check container logs:
```bash
# Find running containers
docker ps | grep celestia

# Check logs
docker logs <container-name>
```

Access container shell:
```bash
docker exec -it <container-name> /bin/bash
```

## Development Tips

### Fast Development Loop

For rapid testing during development:

1. **Use build caching**: Docker layers cache build steps
2. **Run specific tests**: Target the test you're working on
3. **Use existing images**: When possible, use published images to avoid build time

### Test Writing

When creating new docker-e2e tests:

1. Follow the pattern in existing tests
2. Use the `CelestiaTestSuite` structure
3. Include proper cleanup with `t.Cleanup()`
4. Test both success and failure scenarios

### Resource Management

The tests create Docker containers and networks:

- Containers are automatically cleaned up after tests
- Networks are shared across test suite
- Images persist and can be reused

## Related Documentation

- [Tastora Framework](https://github.com/celestiaorg/tastora): The underlying test framework
- [Celestia App](https://github.com/celestiaorg/celestia-app): Main application repository
- [Celestia Core](https://github.com/celestiaorg/celestia-core): Consensus layer (fork of CometBFT)
- [Docker Documentation](https://docs.docker.com/): For Docker-specific issues

## Contributing

When adding new docker-e2e tests:

1. Follow Go testing conventions
2. Use descriptive test names
3. Include proper documentation
4. Test both positive and negative cases
5. Ensure tests are deterministic and can run in parallel

For questions or issues, please open an issue in the [Celestia App repository](https://github.com/celestiaorg/celestia-app/issues).