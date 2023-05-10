# E2E Framework

## Purpose

The e2e package provides a framework for integration testing of the Celestia consensus network.
It consists of a simple CLI and a series of TOML testnet files which manage the running of
several instances within the same network. The e2e test suite has the following purposes in mind:

- **Compatibility testing**: Ensuring that multiple minor versions can operate successfully together within the same network.
- **Upgrade testing**: Ensure upgrades, whether major or minor, can perform seamlessly.
- **Sync testing**: Ensure that the latest version can sync data from the entire chain.
- **Non determinism check**: Ensure that the state machine is free of non-determinism that could compromise replication.
- **Invariant checking**: Ensure that system wide invariants hold.
  
The e2e package is designed predominantly for correctness based testing of small clusters of node.
It is designed to be relatively quick and can be used locally. It relies on docker and docker compose
to orchestrate the nodes.

## Usage

To get started, run `make` within the e2e package directory. This builds the image referring to the current
branch as well as the cli (To build just the cli run `make cli`). Then, to run the complete suite:

```bash
./build/e2e -f networks/simple.toml
```

You should see something like

```bash
Setting up network simple-56602
Spinning up testnet
Starting validator01 on <http://localhost:4202>
Starting validator02 on <http://localhost:4203>
Starting validator03 on <http://localhost:4204>
Starting validator04 on <http://localhost:4205>
Starting full01 on <http://localhost:4201>
Waiting for the network to reach height 20
Stopping testnet
Finished testnet successfully
```

Alternatively you can use the commands: `setup`, `start`, `stop`, and `cleanup`.
