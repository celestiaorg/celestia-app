# Benchmarks

This package contains benchmarks for the ABCI methods with the following transaction types:

- Message send
- IBC update client
- PayForBlobs

## How to Run

To run the benchmarks, run the following in the root directory:

```shell
go test -tags=bench_abci_methods -bench=<benchmark_name> app/benchmarks/benchmark_*
```

## Results

The results are outlined in the [results](results.md) document.

## Key takeaways

We decided to softly limit the number of messages contained in a block, by introducing the `MaxPFBMessages` and `MaxNonPFBMessages`, and checking against them while preparing the proposal.

This way, the default block construction mechanism will only propose blocks that respect these limitations. And if a block that doesn't respect them reaches consensus, it will still be accepted since this rule is not consensus breaking.

As specified in the [results](results.md) document, those results were generated on a 16-core, 48GB RAM machine and gave us certain thresholds. However, when we ran the same experiments on the recommended validator setup, with a 4-core, 16GB RAM machine, the numbers were lower. These low numbers are what we used in the limits.
