# Testground Experiment Tooling

## Test Instance Communication and Experiment Flow

[Context](https://github.com/celestiaorg/celestia-app/blob/d698845db9b28cbacef2e5bde57ef9dc443fc21a/test/testground/network/role.go#L18-L36)

```mermaid
sequenceDiagram
    participant I as Initializer Node
    participant L as Leader Node
    participant F1 as Follower Node 1
    participant F2 as Follower Node 2
    participant Fn as Follower Node N

    Note over I, Fn: Testground Initialization
    I->>L: Create Leader Node Instance
    I->>F1: Create Follower Node 1 Instance
    I->>F2: Create Follower Node 2 Instance
    I->>Fn: Create Follower Node N Instance

    Note over L, Fn: EntryPoint(runenv *runtime.RunEnv, initCtx *run.InitContext)
    
    Note over L, Fn: Plan(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext)
    F1->>L: Send PeerPacket
    F2->>L: Send PeerPacket
    Fn->>L: Send PeerPacket

    Note over L: Genesis Creation
    L->>L: Collect GenTx

    L->>F1: Send Genesis File
    L->>F2: Send Genesis File
    L->>Fn: Send Genesis File

    Note over L: Configuration
    L->>L: Configurators

    L->>F1: Send Config Files
    L->>F2: Send Config Files
    L->>Fn: Send Config Files

    Note over L, Fn: Start Network

    Note over L, Fn: Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext)

    L->>F1: Send Arbitrary Commands
    L->>F2: Send Arbitrary Commands
    L->>Fn: Send Arbitrary Commands

    L->>F1: Send EndTest Command
    L->>F2: Send EndTest Command
    L->>Fn: Send EndTest Command

    Note over L, Fn: Retro(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext)

    Note over L: Process log local data
```

## Configuring an Experiment

### Defining Topologies and Configs

Per the diagram above, the leader node initializes and modifies the configs used
by each node. This allows for arbitrary network topologies to be created.

## Implemented Experiments

### Standard

The `standard` test runs an experiment that is as close to mainnet as possible.
This is used as a base for other experiments.

## Running the Experiment

Testground must be installed, and testground cluster must be setup in a
kubernetes cluster that you have access to via a kubeconfig file. More details
can be found in the [testground](https://github.com/testground/testground) repo.

```sh
cd ./test/testground
testground plan import --from . --name core-app

# This command should be executed in the 1st terminal
testground daemon

# This command should be executed in the 2nd terminal
testground run composition -f compositions/standard/plan.toml --wait

# After the test has been completed, run this command to cleanup remaining instance resources
testground terminate --runner cluster:k8s
```

## Collecting Data

### Grafana

All metrics data is logged to a separate testground specific grafana/influx
node. To access that node, forward the ports using kubectl.

```sh
export POD_NAME=$(kubectl get pods --namespace default -l "app.kubernetes.io/name=grafana,app.kubernetes.io/instance=tg-monitoring" -o jsonpath="{.items[0].metadata.name}")

kubectl --namespace default port-forward $POD_NAME 3000

contact members of the devops team or testground admins to get the creds for accessing this node.
```

### Tracing

The tracing infrastructure in celestia-core can be used by using `tracing_nodes`
plan parameter greater than 0, along with specifying the tracing URL and tracing
token as plan parameters in the `plan.toml`.
