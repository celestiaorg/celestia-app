# End-to-End Testing

Celestia uses the [knuu](https://github.com/celestiaorg/knuu) framework to orchestrate clusters of nodes in a network for end-to-end testing. This relies on Docker and a kubeconfig (in `~/.kube/config`) to access the Kubernetes cluster.

End-to-end tests pull docker images from `ghcr.io/celestiaorg/celestia-app`. These are automatically published when tagging a new release or when opening a pull request. If you wish to manually test a specific commit, you can manually publish the image by first running `make build-ghcr-docker` (from the root directory) and then running `make publish-ghcr-docker`. You must have permission to push to the `ghcr.io/celestiaorg/celestia-app` repository.

## Usage

**Prerequisite: Requires a kubeconfig file.**

You can run the End-to-End tests using either of the following commands:

```shell
go run ./test/e2e```

```shell
make test-e2e
```

To run a specific test, you can pass the name of the test as a command-line argument. For example, to run the "E2ESimple" test, you would use either of the specified commands:

```shell
go run ./test/e2e E2ESimple
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

## Running locally

It is also possible to run the whole workloads locally using minikube.

### Backup existing configuration

By default, the instructions below will overwrite your existing cluster configuration. To back up your configuration:

```shell
cp ${HOME}/.kube/config ${HOME}/.kube/config_backup
```

This will back up your default kubernetes configuration. If you use a different directory/file, and you plan on referencing it when creating the minikube cluster below, please back it up.

### Install minikube

Minikube is required to be installed on your machine. If you have a linux machine, follow the [minikube docs](https://kubernetes.io/fr/docs/tasks/tools/install-minikube/). If you're on macOS ARM, this [tutorial](https://devopscube.com/minikube-mac/) can be helpful to run it using qemu.

### Create namespace

The command in [usage](#usage) specifies an environment variable `KNUU_NAMESPACE` to the value `test`. This namespace will need to be created before running that command:

```shell
kubectl create namespace test
```

If another namespace is to be used, please create it using the same command while changing `test` to your target namespace.

### Check the logs

After you start the E2E tests, you can check if you have the validators running:

```shell
kubectl get pods --namespace test
NAME                             READY   STATUS    RESTARTS   AGE
timeout-handler-09e1a426-jwm7n   1/1     Running   0          2m18s
timeout-handler-921a1b93-52g6d   1/1     Running   0          40m
timeout-handler-c3442f46-lvxw2   1/1     Running   0          37m
timeout-handler-f5ccc2c9-7hld2   1/1     Running   0          34m
val0-3a7e2e1e-zs8h5              1/1     Running   0          60s
val1-9b802df8-tcvnb              1/1     Running   0          51s
val2-91b57a4d-ht57t              1/1     Running   0          42s
val3-dcc2ef6c-cg8k5              1/1     Running   0          32s
```

The logs can be checked using:

```shell
kubectl logs --namespace test -f <pod_name>
```

With `<pod_name>` being a pod name like `val0-3a7e2e1e-zs8h5`.

### Destroy the pods

By default, the pods will be killed automatically after 60 minutes. However, if you want to clean up the cluster manually, and destroy everything:

```shell
kubectl delete pods --all --namespace test
```

Note: This will delete all the created pods in the default kubernetes cluster under the `test` namespace. Make sure to run it against the correct cluster and double-check the pods list that is going to be destroyed using this command:

```shell
kubectl get pods --namespace test
```

### Restoring old cluster configuration

To restore your previous cluster configuration, if you followed the [backup your existing cluster configuration](#backup-existing-configuration) section:

```shell
cp ${HOME}/.kube/config_backup ${HOME}/.kube/config
```
