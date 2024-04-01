# End to End Testing

Celestia uses the [knuu](https://github.com/celestiaorg/knuu) framework to orchestrate clusters of nodes in a network for end to end testing. This relies on Docker and a kubeconfig (in `~/.kube/config`) to access the Kubernetes cluster.

End to end tests pull docker images from ghcr.io/celestiaorg/celestia-app. These are automatically published when tagging a new release or when opening a pull request. If you wish to manually test a specific commit, you can manually publish the image by first running `make build-ghcr-docker` (from the root directory) and then running `make publish-ghcr-docker`. You must have permission to push to the ghcr.io/celestiaorg/celestia-app repository.

## Usage

E2E tests can be simply run through go tests. They are distinguished from unit tests through an environment variable. To run all e2e tests run:

```shell
KNUU_NAMESPACE=test E2E_LATEST_VERSION="$(git rev-parse --short main)" E2E_VERSIONS="$(git tag -l)"  go test ./test/e2e/... -timeout 30m -v
```

You can optionally set a global timeout using `KNUU_TIMEOUT` (default is 60m).

## Observation

Logs of each of the nodes are posted to Grafana and can be accessed through Celestia's dashboard (using the `test` namespace).

### Metrics

To view the metrics from the testnet, you should set the `GRAFANA_ENDPOINT`, `GRAFANA_USERNAME`, and `GRAFANA_TOKEN` environment variables. This uses Prometheus alongside the Jaeger and Otlp Exporter.
