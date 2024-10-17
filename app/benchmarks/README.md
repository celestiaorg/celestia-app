# Benchmarks

This package contains benchmarks for the ABCI methods with the following transaction types:

- Message send
- IBC update client
- PayForBlobs

## How to run

To run the benchmarks, run the following in the root directory:

```shell
go test -tags=bench_prepare_proposal -bench=<benchmark_name> app/benchmarks/benchmark_*
```

## Results

The results are outlined in the [results](results.md) document.

## Key takeaways

We decided to softly limit the number of messages contained in a block, via introducing the `NonPFBTransactionCap` and `PFBTransactionCap`, and checking against them in prepare proposal.

This way, the default block construction mechanism will only propose blocks that respect these limitations. And if a block that doesn't respect them reached consensus, it will still be accepted since this rule is not consensus breaking.

As specified in [results](results.md) documents, those results were generated on 16 core 48GM RAM machine, and gave us certain thresholds. However, when we run the same experiments on the recommended validator setup, 4 cores 16GB RAM, the numbers were lower. These low numbers are what we used in the limits.
