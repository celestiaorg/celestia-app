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

The message count is frozen at 5,800 (`pffCounts`), which the validator sweep
reuses so that it varies only the validator set. It is not a ladder: it exists so that
every PR in the campaign reports one comparable number, not to locate the
message count at which a phase crosses a time budget - that needs a sweep this
harness does not have.

The `benchmarks` build tag lifts `appconsts.MaxPayForFibreMessages` from 200 to
a number no block reaches. That changes three bounds, not one: the square
builder's cap, ProcessProposal's rejection, **and the coverage limit on the
parallel pre-verification pass**. Under production constants the pass would
cover 200 messages and the rest would verify sequentially, so this harness
cannot measure a change to that bound.

`PrepareProposal`, `ProcessProposal` and `FinalizeBlock` are swept over two
cache states, and the gap between them is what the signature caching work is
worth. `CheckTx`, `Commit` and the validator sweep have one arm each:

- `cache=cold` is a restarted or lagging node. Every cache **the application
  owns** is emptied - signatures, blob txs, PayForFibre txs, proposal
  artifacts and converted validator sets - so every transaction is processed
  in full. Three process-global caches survive and are warm in both arms:
  CometBFT's expanded-public-key LRU in `crypto/ed25519`, the SDK's bech32
  address caches, and the tree pool. A real restarted node refills the first
  two within its first few transactions, so the gap this measures is still the
  application's, but "cold" is not a cold process.
- `cache=warm` is a validator with the relevant caches already populated.
  For ProcessProposal, the preceding cold call populates the same signature
  and validator-set caches that CheckTx would, avoiding a second untimed pass
  over every transaction. It is not the proposer: setup purges the artifacts
  PrepareProposal left behind.

Pin cores with `taskset`, not `go test -cpu`: the parallel verifier sizes its
fan-out from `runtime.NumCPU()`, which `GOMAXPROCS` does not change.

```shell
taskset -c 0-15 go test -tags=benchmarks -run='^$' -bench=_PFF \
  -benchtime=5x -count=10 -timeout=180m ./app/benchmarks/
```

`-benchtime` and `-timeout` are not optional: at the default benchtime a single
`-count` of this suite runs past `go test`'s ten-minute limit. One sample is
also not a measurement - the same binary on the same machine has been seen to
swing 2x between runs, enough to report `cache=warm` as slower than
`cache=cold`. Take the median of at least ten, which is what
`scripts/pff_campaign_run.sh` does.

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
