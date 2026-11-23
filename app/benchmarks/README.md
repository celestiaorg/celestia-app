# Benchmarks

This package contains benchmarks for the ABCI methods with the following transaction types:

- Message send
- IBC update client
- PayForBlobs

## How to Run

To run the benchmarks, run the following in the root directory:

```shell
go test -tags=benchmarks -bench=<benchmark_name> app/benchmarks/...
```

## Results

The results are outlined in the [results](results.md) document.

## Key takeaways

We decided to softly limit the number of messages contained in a block, by introducing the `MaxPFBMessages` and `MaxSDKMessages`, and checking against them while preparing the proposal.

This way, the default block construction mechanism will only propose blocks that respect these limitations. ProcessProposal also rejects a block that exceeds them, so they are consensus breaking.

As specified in the [results](results.md) document, those results were generated on a 16-core, 48GB RAM machine and gave us certain thresholds. However, when we ran the same experiments on the recommended validator setup, with a 4-core, 16GB RAM machine, the numbers were lower. These low numbers are what we used in the limits.
