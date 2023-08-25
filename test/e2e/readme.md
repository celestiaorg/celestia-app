# End to End Testing

Celestia uses the [knuu](https://github.com/celestiaorg/knuu) framework to orchestrate clusters of nodes in a network for end to end testing. This relies on Docker and a kubeconfig (in `~/.kube/config`) to access the Kubernetes cluster.

## Usage

E2E tests can be simply run through go tests. They are distinguished from unit tets through an environment variable. To run all e2e tests run:

```shell
E2E=true KNUU_NAMESPACE=knuu-app go test ./test/e2e/... -timeout 30m
```

## Observation

Logs of each of the nodes are posted to Grafana and can be accessed through Celestia's dashboard (using the `celestia-app` namespace).
