# Chaos test

`TestChaos` in [test/docker-e2e](../test/docker-e2e/e2e_chaos_test.go) starts 4
validators and a full node, runs txsim against the full node, and injects a
fault schedule into the validators. It runs every night in
[test-nightly.yml](../.github/workflows/test-nightly.yml).

A seed gives the schedule: each step has a fault, its target validators and a
duration of 5 to 30 seconds. A `partition` takes the node's `eth0` down, `delay`
and `loss` use `tc netem`, and `pause` uses `docker pause`. The `ip` and `tc`
commands run in a short-lived container of Pumba's network tools image that
joins the node's network namespace with `NET_ADMIN`, so the celestia-app image
needs no extra tools. A step faults less than 1/3 of the voting power, except
one "halt then heal" step that partitions or pauses 2 of 4 validators.

After each heal, the chain must advance 3 blocks and the healed validators must
catch up within 2 minutes. The consistency monitor checks that all nodes agree
on each height. At the end, the test checks the app hash on every validator.

## Run and replay

```sh
CELESTIA_IMAGE=ghcr.io/celestiaorg/celestia-app CELESTIA_TAG=<tag> \
  make test-docker-e2e test=TestChaos
```

The test logs its seed and schedule at the start and on failure. Set
`CHAOS_SEED` to replay a schedule; heights and timing still differ between runs.

| Variable | Effect |
| --- | --- |
| `CHAOS_SEED` | Seed of the schedule. If not set, the test picks a random seed. |
| `CHAOS_STEPS` | Number of steps (default 8). |
| `CHAOS_MAX_FAULT` | Longest fault, such as `15s` (default 30s). |
| `CHAOS_REQUIRE_NETEM` | Fail if the kernel has no netem, instead of skipping delay and loss steps. The nightly sets it. |
| `CHAOS_LOG_DIR` | On failure, write each node's full log to this directory instead of logging the tail. |
