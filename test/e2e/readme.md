# End-to-End Testing

Celestia uses Docker-based end-to-end testing to orchestrate clusters of nodes in a network for testing. The tests use Docker containers to simulate a Celestia network environment and validate functionality.

End-to-end tests pull docker images from `ghcr.io/celestiaorg/celestia-app` and `ghcr.io/celestiaorg/txsim`. These are automatically published when tagging a new release or when opening a pull request. If you wish to manually test a specific commit, you can manually publish the image by first running `make build-ghcr-docker` (from the root directory) and then running `make publish-ghcr-docker`. You must have permission to push to the `ghcr.io/celestiaorg/celestia-app` repository.

## Usage

**Prerequisite: Requires Docker.** Make sure Docker is installed and running on your system.

You can run the End-to-End tests using the following command:

```shell
make test-e2e
```

To run a specific test, you can pass the test name as a parameter. For example, to run the "TestE2ESimple" test:

```shell
make test-docker-e2e test=TestE2ESimple
```

To run all tests without the short mode restriction:

```shell
cd test/docker-e2e && go test -v ./...
```

**Optional environment variables**:

- `CELESTIA_IMAGE` can be used to override the default celestia-app Docker image
- `CELESTIA_TAG` can be used to override the default image tag

## Test Structure

The end-to-end tests are located in the `test/docker-e2e` directory and include:

- `TestE2ESimple`: Basic functionality test that creates a network and submits transactions
- `TestStateSync`: Tests state synchronization functionality

## Running Locally

The tests run entirely in Docker containers and do not require additional infrastructure setup. Simply ensure Docker is running and execute the test commands above.

### Viewing Logs

Test logs are displayed in the console output. For more detailed debugging, you can check Docker container logs:

```shell
docker ps # to see running containers
docker logs <container_id> # to view logs for a specific container
```

### Cleanup

Docker containers and networks are automatically cleaned up after tests complete. If you need to manually clean up:

```shell
docker system prune # removes unused containers, networks, and images
```
