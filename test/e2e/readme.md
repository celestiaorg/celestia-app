# End to End Testing

Celestia uses the [knuu](https://github.com/celestiaorg/knuu) framework to orchestrate clusters of nodes in a network for end to end testing. This relies on Docker and a kubeconfig (in `~/.kube/config`) to access the Kubernetes cluster.

End to end tests pull docker images from ghcr.io/celestiaorg/celestia-app. These are automatically published when tagging a new release or when opening a pull request. If you wish to manually test a specific commit, you can manually publish the image by first running `make build-ghcr-docker` (from the root directory) and then running `make publish-ghcr-docker`. You must have permission to push to the ghcr.io/celestiaorg/celestia-app repository.

## Usage

**Prerequisite: Requires a kubeconfig file.**

You can run the End-to-End tests using either of the following commands:

```shell
go run test/e2e/*.go -timeout 30m -v
```

```shell
make test-e2e
```

To run a specific test, you can pass the name of the test as a command-line argument. For example, to run the "E2ESimple" test, you would use either of the specified commands:

```shell
go run test/e2e/*.go E2ESimple
```

```shell
make test-e2e E2ESimple  
```

**Optional parameters**:

- `KNUUU_TIMEOUT` can be used to override the default timeout of 60 minutes for the tests.

## Observation

Logs of each of the nodes are posted to Grafana and can be accessed through Celestia's dashboard (using the `test` namespace).

### Metrics

To view the metrics from the testnet, you should set the `GRAFANA_ENDPOINT`, `GRAFANA_USERNAME`, and `GRAFANA_TOKEN` environment variables. This uses Prometheus alongside the Jaeger and Otlp Exporter.
