# Quantum Gravity Bridge end to end integration test

This directory contains the QGB e2e integration tests. It serves as a way to fully test the QGB orchestrator and relayer in real network scenarios

## Topology

as discussed under [#398](https://github.com/celestiaorg/celestia-app/issues/398) The e2e network defined under `qgb_network.go` has the following components:

- 4 Celestia-app nodes that can be validators
- 4 Orchestrator nodes that will each run aside of a celestia-app
- 1 Ethereum node. Probably Ganache as it is easier to set up
- 1 Relayer node that will listen to Celestia chain and relay attestations
- 1 Deployer node that can deploy a new QGB contract when needed.

For more information on the environment variables required to run these tests, please check the `docker-compose.yml` file and the shell scripts defined under `celestia-app` directory.

## How to run

### Requirements

To run the e2e tests, a working installation of [docker-compose](https://docs.docker.com/compose/install/) is  needed.

### Makefile

A Makefile has been defined under this directory to run the tests, with a `test` target:

```shell
make test
```

### Run a specific test

To run a single test, run the following:

```shell
QGB_INTEGRATION_TEST=true go test -mod=readonly -test.timeout 30m -v -run <test_name>
```

### Run all the tests using `go` directly

```shell
QGB_INTEGRATION_TEST=true go test -mod=readonly -test.timeout 30m -v
```

## Common issues

Currently, when the tests are run using the above ways, there are possible issues that might happen.

### hanging docker containers after a sudden network stop

If the tests were stopped unexpectidely, for example, sending a `SIGINT`, ie, `ctrl+c`, the resources will not be releases correctly (might be fixed in the future). This will result in seeing similar logs to the following :

```text
ERROR: for core0  Cannot create container for service core0: Conflict. The container name "/core0" is already in use by container "4bdaf40e2cd26bf549738ea95f53ba49cb5407c3d892b50b5a75e72e08e
3e0a8". You have to remove (or rename) that container to be able to reuse that name.                                                                                                          
Host is already in use by another container                                                                                                                                                   
Creating 626fbf28-7c90-4842-be8e-3346f864b369_ganache_1 ... error                                                                                                                             
                                                                                                                                                                                              
ERROR: for 626fbf28-7c90-4842-be8e-3346f864b369_ganache_1  Cannot start service ganache: driver failed programming external connectivity on endpoint 626fbf28-7c90-4842-be8e-3346f864b369_gana
che_1 (23bf2faf8fbce45f4a112b59183739f294c0e2d4fb208fec89e4805f3d719381): Bind for 0.0.0.0:8545 failed: port is already allocated                                                             
                                                                                                                                                                                              
ERROR: for core0  Cannot create container for service core0: Conflict. The container name "/core0" is already in use by container "4bdaf40e2cd26bf549738ea95f53ba49cb5407c3d892b50b5a75e72e08e
3e0a8". You have to remove (or rename) that container to be able to reuse that name.                                                                                                          
                                                                                                                                                                                              
ERROR: for ganache  Cannot start service ganache: driver failed programming external connectivity on endpoint 626fbf28-7c90-4842-be8e-3346f864b369_ganache_1 (23bf2faf8fbce45f4a112b59183739f294c0e2d4fb208fec89e4805f3d719381): Bind for 0.0.0.0:8545 failed: port is already allocated
Encountered errors while bringing up the project.
Attaching to 626fbf28-7c90-4842-be8e-3346f864b369_ganache_1
```

To fix it, run the `cleanup.sh` script under `scripts` directory :

```shell
./scripts/cleanup.sh
```

NB : This will kill and remove hanging containers and networks related to the executed. But, might also delete unrelated ones if they have the same name.
