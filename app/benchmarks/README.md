# Benchmarks

This package contains benchmarks for the ABCI methods with the following transaction types:

- Message send
- IBC update client
- PayForBlobs
- PayForFibre

## How to Run

To run the benchmarks, run the following in the root directory:

```shell
go test -tags=benchmarks -bench=<benchmark_name> app/benchmarks/...
```

## PayForFibre

`benchmark_pff_test.go` measures each ABCI call over a block of PayForFibre
messages carrying a real quorum certificate, swept over messages per block and
over validator set size.

The sweep tops out at 5,848 messages, the most that fit a 256 square; see the
comment on `pffCounts`. The protocol limit is lower still - 2,000 at app v11 -
so the points above it measure capacity the chain does not yet allow, which is
the point of the exercise.

Every benchmark is swept over two cache states, and the gap between them is
what the signature caching work is worth:

- `cache=cold` is a restarted or lagging node. Every cache the node keeps in
  memory is emptied - signatures, blob txs, PayForFibre txs, proposal
  artifacts and converted validator sets - so every transaction is processed
  in full. For `FinalizeBlock` this is the block replay path, which never ran
  ProcessProposal.
- `cache=warm` is a validator with the relevant caches already populated.
  For ProcessProposal, the preceding cold call populates the same signature
  and validator-set caches that CheckTx would, avoiding a second untimed pass
  over every transaction. It is not the proposer: setup purges the artifacts
  PrepareProposal left behind.

Pin cores with `taskset`, not `go test -cpu`: the parallel verifier sizes its
fan-out from `runtime.NumCPU()`, which `GOMAXPROCS` does not change.

```shell
taskset -c 0-15 go test -tags=benchmarks -run='^$' -bench=_PFF -count=10 ./app/benchmarks/
```

Unlike the older benchmarks in this package, these iterate with `b.Loop()`, so
`ns/op` is a real per-operation time at any `-benchtime`.

## Results

The results are outlined in the [results](results.md) document.

Note: the figures in `results.md` come from benchmarks that ignore `b.N`, so
the framework divides by a count that never ran - hence a single `checkTx`
recorded there as "0.0003585 ns". Do not copy that style.

## Key takeaways

We decided to softly limit the number of messages contained in a block, by introducing the `MaxPFBMessages` and `MaxSDKMessages`, and checking against them while preparing the proposal.

This way, the default block construction mechanism will only propose blocks that respect these limitations. And if a block that doesn't respect them reaches consensus, it will still be accepted since this rule is not consensus breaking.

As specified in the [results](results.md) document, those results were generated on a 16-core, 48GB RAM machine and gave us certain thresholds. However, when we ran the same experiments on the recommended validator setup, with a 4-core, 16GB RAM machine, the numbers were lower. These low numbers are what we used in the limits.
