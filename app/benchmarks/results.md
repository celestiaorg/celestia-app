<!-- markdownlint-disable -->
# Benchmark results

This document contains the results of the benchmarks defined under `app/benchmarks`.

The benchmarks were run on a Macbook Pro M3 MAX with 48GB RAM.

The benchmarks will be run using an in memory DB, then a local db, goleveldb.

## In memory DB benchmarks

### `sendMsg` benchmarks

#### CheckTx

A single `checkTx` of a `sendMsg` message takes 0.0003585 **ns** to execute. And it uses 74374 gas.

The transactions in an `8mb` block containing 31645 `sendMsg` messages take 6,29 s (6293858682 ns) to run `checkTx` on all of them. The total gas used is 1884371034 gas.

#### DeliverTx

A single `deliverTx` of a `sendMsg` message takes 0.0002890 **ns** to execute. And it uses 103251 gas.

The transactions in an `8mb` block containing 31645 `sendMsg` messages take 7,56 s (7564111078 ns) to run `deliverTx` on all of them. The total gas used is 2801272121 gas.

#### PrepareProposal

A single `prepareProposal` of a `sendMsg` message takes 0.0002801 **ns** to execute. And it uses 101110 gas.

An `8mb` block containing 31645 `sendMsg` messages takes 5,04 s (5049140917 ns) to execute. The total gas used 1843040790 gas.

#### ProcessProposal

A single `processProposal` of a `sendMsg` message takes 0.0002313 **ns** to execute. And it uses 101110 gas.

An `8mb` block containing 31645 `sendMsg` messages takes 5,17 s (5179850250 ns) to execute. The total gas used 1,843,040,790 gas.

For the processing time of a block full of `sendMsg`, we benchmarked how much time they take depending on the number of transactions, and we have the following results:

| Number of Transactions | ElapsedTime(s) | Number of Transactions | ElapsedTime(s) |
|------------------------|----------------|------------------------|----------------|
| 1650                   | 0.2494         | 1670                   | 0.2594         |
| 1690                   | 0.2628         | 1739                   | 0.2723         |
| 1761                   | 0.2732         | 1782                   | 0.2770         |
| 1856                   | 0.2878         | 1878                   | 0.2976         |
| 1901                   | 0.2990         | 1956                   | 0.3023         |
| 1980                   | 0.3076         | 2004                   | 0.3232         |
| 2062                   | 0.3252         | 2088                   | 0.3257         |
| 2112                   | 0.3326         | 2138                   | 0.3417         |
| 2200                   | 0.3398         | 2227                   | 0.3495         |
| 2254                   | 0.3545         | 2319                   | 0.3688         |
| 2349                   | 0.3684         | 2376                   | 0.3771         |
| 2475                   | 0.3972         | 2505                   | 0.3928         |
| 2535                   | 0.4080         | 2608                   | 0.4098         |
| 2641                   | 0.4123         | 2673                   | 0.4135         |
| 2750                   | 0.4614         | 2784                   | 0.4333         |
| 2817                   | 0.4537         | 2851                   | 0.4530         |
| 2934                   | 0.4633         | 2970                   | 0.4623         |
| 3006                   | 0.4863         | 3093                   | 0.4821         |
| 3132                   | 0.4888         | 3168                   | 0.4962         |
| 3207                   | 0.5058         | 3300                   | 0.5119         |
| 3340                   | 0.5275         | 3381                   | 0.5280         |
| 3478                   | 0.5441         | 3523                   | 0.5473         |
| 3564                   | 0.5546         | 3712                   | 0.5743         |
| 3757                   | 0.6081         | 3802                   | 0.5970         |
| 3912                   | 0.6093         | 3961                   | 0.6125         |
| 4009                   | 0.6329         | 4125                   | 0.6663         |
| 4176                   | 0.6395         | 4225                   | 0.6615         |
| 4276                   | 0.6844         | 4401                   | 0.7190         |
| 4455                   | 0.6943         | 4509                   | 0.7006         |
| 4639                   | 0.7219         | 4698                   | 0.7365         |
| 4752                   | 0.7340         | 5500                   | 0.8489         |

### `PFB` benchmarks

#### CheckTx: `BenchmarkCheckTx_PFB_Multi`

Benchmarks of `CheckTx` for a single PFB with different sizes:

| Benchmark Name                              | Time (ns/op) | Gas Used | Transaction Size (Bytes) | Transaction Size (MB) |
|---------------------------------------------|--------------|----------|--------------------------|-----------------------|
| BenchmarkCheckTx_PFB_Multi/300_bytes-16     | 0.0003121 ns | 74,664   | 703                      | 0.000703 MB           |
| BenchmarkCheckTx_PFB_Multi/500_bytes-16     | 0.0003392 ns | 74,664   | 903                      | 0.000903 MB           |
| BenchmarkCheckTx_PFB_Multi/1000_bytes-16    | 0.0002797 ns | 74,664   | 1,403                    | 0.001403 MB           |
| BenchmarkCheckTx_PFB_Multi/5000_bytes-16    | 0.0002818 ns | 74,664   | 5,403                    | 0.005403 MB           |
| BenchmarkCheckTx_PFB_Multi/10000_bytes-16   | 0.0003094 ns | 74,664   | 10,403                   | 0.010403 MB           |
| BenchmarkCheckTx_PFB_Multi/50000_bytes-16   | 0.0004127 ns | 74,674   | 50,406                   | 0.050406 MB           |
| BenchmarkCheckTx_PFB_Multi/100000_bytes-16  | 0.0004789 ns | 74,674   | 100,406                  | 0.100406 MB           |
| BenchmarkCheckTx_PFB_Multi/200000_bytes-16  | 0.0006958 ns | 74,674   | 200,406                  | 0.200406 MB           |
| BenchmarkCheckTx_PFB_Multi/300000_bytes-16  | 0.0008678 ns | 74,674   | 300,406                  | 0.300406 MB           |
| BenchmarkCheckTx_PFB_Multi/400000_bytes-16  | 0.001076 ns  | 74,674   | 400,406                  | 0.400406 MB           |
| BenchmarkCheckTx_PFB_Multi/500000_bytes-16  | 0.001307 ns  | 74,674   | 500,406                  | 0.500406 MB           |
| BenchmarkCheckTx_PFB_Multi/1000000_bytes-16 | 0.002291 ns  | 74,674   | 1,000,406                | 1.000406 MB           |
| BenchmarkCheckTx_PFB_Multi/2000000_bytes-16 | 0.005049 ns  | 74,674   | 2,000,406                | 2.000406 MB           |
| BenchmarkCheckTx_PFB_Multi/3000000_bytes-16 | 0.006911 ns  | 74,684   | 3,000,409                | 3.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/4000000_bytes-16 | 0.008246 ns  | 74,684   | 4,000,409                | 4.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/5000000_bytes-16 | 0.01127 ns   | 74,684   | 5,000,409                | 5.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/6000000_bytes-16 | 0.01316 ns   | 74,684   | 6,000,409                | 6.000409 MB           |

#### DeliverTx: `BenchmarkDeliverTx_PFB_Multi`

Benchmarks of `DeliverTx` for a single PFB with different sizes:

| Benchmark Name                                | Time (ns/op) | Gas Used   | Transaction Size (Bytes) | Transaction Size (MB) |
|-----------------------------------------------|--------------|------------|--------------------------|-----------------------|
| BenchmarkDeliverTx_PFB_Multi/300_bytes-16     | 0.0002718 ns | 77,682     | 703                      | 0.000703 MB           |
| BenchmarkDeliverTx_PFB_Multi/500_bytes-16     | 0.0002574 ns | 81,778     | 903                      | 0.000903 MB           |
| BenchmarkDeliverTx_PFB_Multi/1000_bytes-16    | 0.0002509 ns | 85,874     | 1,403                    | 0.001403 MB           |
| BenchmarkDeliverTx_PFB_Multi/5000_bytes-16    | 0.0002755 ns | 118,642    | 5,403                    | 0.005403 MB           |
| BenchmarkDeliverTx_PFB_Multi/10000_bytes-16   | 0.0002726 ns | 159,602    | 10,403                   | 0.010403 MB           |
| BenchmarkDeliverTx_PFB_Multi/50000_bytes-16   | 0.0002795 ns | 499,580    | 50,406                   | 0.050406 MB           |
| BenchmarkDeliverTx_PFB_Multi/100000_bytes-16  | 0.0002488 ns | 925,564    | 100,406                  | 0.100406 MB           |
| BenchmarkDeliverTx_PFB_Multi/200000_bytes-16  | 0.0002487 ns | 1,773,436  | 200,406                  | 0.200406 MB           |
| BenchmarkDeliverTx_PFB_Multi/300000_bytes-16  | 0.0002887 ns | 2,625,404  | 300,406                  | 0.300406 MB           |
| BenchmarkDeliverTx_PFB_Multi/400000_bytes-16  | 0.0002810 ns | 3,473,276  | 400,406                  | 0.400406 MB           |
| BenchmarkDeliverTx_PFB_Multi/500000_bytes-16  | 0.0002616 ns | 4,325,244  | 500,406                  | 0.500406 MB           |
| BenchmarkDeliverTx_PFB_Multi/1000000_bytes-16 | 0.0003983 ns | 8,572,796  | 1,000,406                | 1.000406 MB           |
| BenchmarkDeliverTx_PFB_Multi/2000000_bytes-16 | 0.0003368 ns | 17,071,996 | 2,000,406                | 2.000406 MB           |
| BenchmarkDeliverTx_PFB_Multi/3000000_bytes-16 | 0.0005770 ns | 25,571,206 | 3,000,409                | 3.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/4000000_bytes-16 | 0.0003752 ns | 34,066,310 | 4,000,409                | 4.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/5000000_bytes-16 | 0.0003788 ns | 42,565,510 | 5,000,409                | 5.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/6000000_bytes-16 | 0.0003975 ns | 51,064,710 | 6,000,409                | 6.000409 MB           |

#### PrepareProposal: `BenchmarkPrepareProposal_PFB_Multi`

The benchmarks for `PrepareProposal` for 8mb blocks containing PFBs of different sizes:

| Benchmark Name                                                         | Block Size (MB) | Number of Transactions | Prepare Proposal Time (s) | Total Gas Used  | Transaction Size (Bytes) | Transaction Size (MB) |
|------------------------------------------------------------------------|-----------------|------------------------|---------------------------|-----------------|--------------------------|-----------------------|
| BenchmarkPrepareProposal_PFB_Multi/15000_transactions_of_300_bytes-16  | 6.239           | 10,318                 | 2.411 s                   | 988,490,895,000 | 703                      | 0.000703 MB           |
| BenchmarkPrepareProposal_PFB_Multi/10000_transactions_of_500_bytes-16  | 5.035           | 6,331                  | 1.710 s                   | 439,343,930,000 | 903                      | 0.000903 MB           |
| BenchmarkPrepareProposal_PFB_Multi/6000_transactions_of_1000_bytes-16  | 5.809           | 4,566                  | 1.033 s                   | 158,174,358,000 | 1,403                    | 0.001403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3000_transactions_of_5000_bytes-16  | 7.188           | 1,413                  | 0.547 s                   | 39,550,179,000  | 5,403                    | 0.005403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/1000_transactions_of_10000_bytes-16 | 7.470           | 758                    | 0.210 s                   | 4,397,393,000   | 10,403                   | 0.010403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/500_transactions_of_50000_bytes-16  | 7.441           | 155                    | 0.127 s                   | 1,100,446,500   | 50,406                   | 0.050406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/100_transactions_of_100000_bytes-16 | 7.368           | 77                     | 0.045 s                   | 44,369,300      | 100,406                  | 0.100406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/100_transactions_of_200000_bytes-16 | 7.260           | 38                     | 0.059 s                   | 44,369,300      | 200,406                  | 0.200406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/50_transactions_of_300000_bytes-16  | 7.161           | 25                     | 0.056 s                   | 11,202,150      | 300,406                  | 0.300406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/50_transactions_of_400000_bytes-16  | 7.254           | 19                     | 0.054 s                   | 11,202,150      | 400,406                  | 0.400406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/30_transactions_of_500000_bytes-16  | 7.157           | 15                     | 0.041 s                   | 4,085,490       | 500,406                  | 0.500406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/10_transactions_of_1000000_bytes-16 | 6.678           | 7                      | 0.031 s                   | 483,230         | 1,000,406                | 1.000406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/5_transactions_of_2000000_bytes-16  | 5.723           | 3                      | 0.032 s                   | 131,790         | 2,000,406                | 2.000406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3_transactions_of_3000000_bytes-16  | 5.723           | 2                      | 0.042 s                   | 52,716          | 3,000,409                | 3.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3_transactions_of_4000000_bytes-16  | 3.815           | 1                      | 0.040 s                   | 52,716          | 4,000,409                | 4.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/2_transactions_of_5000000_bytes-16  | 4.769           | 1                      | 0.039 s                   | 26,358          | 5,000,409                | 5.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/2_transactions_of_6000000_bytes-16  | 5.722           | 1                      | 0.032 s                   | 26,358          | 6,000,409                | 6.000409 MB           |

#### ProcessProposal: `BenchmarkProcessProposal_PFB_Multi`

The benchmarks for `ProcessProposal` for 8mb blocks containing PFBs of different sizes:

| Benchmark Name                                                         | Block Size (MB) | Number of Transactions | Process Proposal Time (s) | Total Gas Used  | Transaction Size (Bytes) | Transaction Size (MB) |
|------------------------------------------------------------------------|-----------------|------------------------|---------------------------|-----------------|--------------------------|-----------------------|
| BenchmarkProcessProposal_PFB_Multi/15000_transactions_of_300_bytes-16  | 6.239           | 10,318                 | 1.767 s                   | 988,490,895,000 | 703                      | 0.000703 MB           |
| BenchmarkProcessProposal_PFB_Multi/10000_transactions_of_500_bytes-16  | 5.035           | 6,331                  | 1.101 s                   | 439,343,930,000 | 903                      | 0.000903 MB           |
| BenchmarkProcessProposal_PFB_Multi/6000_transactions_of_1000_bytes-16  | 5.809           | 4,566                  | 0.820 s                   | 158,174,358,000 | 1,403                    | 0.001403 MB           |
| BenchmarkProcessProposal_PFB_Multi/3000_transactions_of_5000_bytes-16  | 7.188           | 1,413                  | 0.300 s                   | 39,550,179,000  | 5,403                    | 0.005403 MB           |
| BenchmarkProcessProposal_PFB_Multi/1000_transactions_of_10000_bytes-16 | 7.470           | 758                    | 0.185 s                   | 4,397,393,000   | 10,403                   | 0.010403 MB           |
| BenchmarkProcessProposal_PFB_Multi/500_transactions_of_50000_bytes-16  | 7.441           | 155                    | 0.092 s                   | 1,100,446,500   | 50,406                   | 0.050406 MB           |
| BenchmarkProcessProposal_PFB_Multi/100_transactions_of_100000_bytes-16 | 7.368           | 77                     | 0.089 s                   | 44,369,300      | 100,406                  | 0.100406 MB           |
| BenchmarkProcessProposal_PFB_Multi/100_transactions_of_200000_bytes-16 | 7.260           | 38                     | 0.060 s                   | 44,369,300      | 200,406                  | 0.200406 MB           |
| BenchmarkProcessProposal_PFB_Multi/50_transactions_of_300000_bytes-16  | 7.161           | 25                     | 0.048 s                   | 11,202,150      | 300,406                  | 0.300406 MB           |
| BenchmarkProcessProposal_PFB_Multi/50_transactions_of_400000_bytes-16  | 7.254           | 19                     | 0.051 s                   | 11,202,150      | 400,406                  | 0.400406 MB           |
| BenchmarkProcessProposal_PFB_Multi/30_transactions_of_500000_bytes-16  | 7.157           | 15                     | 0.062 s                   | 4,085,490       | 500,406                  | 0.500406 MB           |
| BenchmarkProcessProposal_PFB_Multi/10_transactions_of_1000000_bytes-16 | 6.678           | 7                      | 0.047 s                   | 483,230         | 1,000,406                | 1.000406 MB           |
| BenchmarkProcessProposal_PFB_Multi/5_transactions_of_2000000_bytes-16  | 5.723           | 3                      | 0.043 s                   | 131,790         | 2,000,406                | 2.000406 MB           |
| BenchmarkProcessProposal_PFB_Multi/3_transactions_of_3000000_bytes-16  | 5.723           | 2                      | 0.053 s                   | 52,716          | 3,000,409                | 3.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/3_transactions_of_4000000_bytes-16  | 3.815           | 1                      | 0.047 s                   | 52,716          | 4,000,409                | 4.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/2_transactions_of_5000000_bytes-16  | 4.769           | 1                      | 0.068 s                   | 26,358          | 5,000,409                | 5.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/2_transactions_of_6000000_bytes-16  | 5.722           | 1                      | 0.047 s                   | 26,358          | 6,000,409                | 6.000409 MB           |

### IBC `UpdateClient` benchmarks

#### CheckTx: `BenchmarkIBC_CheckTx_Update_Client_Multi`

The benchmarks of executing `checkTx` on a single transaction containing an IBC `updateClient` with different numbers of required signatures:

| Benchmark Name                                                        | Time (ns/op) | Gas Used  | Number of Validators | Number of Verified Signatures | Transaction Size (Bytes) | Transaction Size (MB) |
|-----------------------------------------------------------------------|--------------|-----------|----------------------|-------------------------------|--------------------------|-----------------------|
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_2-16   | 0.0007940 ns | 108,598   | 2.0                  | 1.0                           | 1,396                    | 0.001396 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_10-16  | 0.002127 ns  | 127,710   | 10.0                 | 6.0                           | 3,303                    | 0.003303 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_25-16  | 0.003694 ns  | 163,430   | 25.0                 | 16.0                          | 6,875                    | 0.006875 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_50-16  | 0.004701 ns  | 222,930   | 50.0                 | 33.0                          | 12,825                   | 0.012825 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_75-16  | 0.004095 ns  | 282,480   | 75.0                 | 50.0                          | 18,780                   | 0.018780 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_100-16 | 0.004112 ns  | 340,928   | 100.0                | 66.0                          | 24,629                   | 0.024629 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_125-16 | 0.007009 ns  | 400,178   | 125.0                | 83.0                          | 30,554                   | 0.030554 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_150-16 | 0.004906 ns  | 460,980   | 150.0                | 100.0                         | 36,630                   | 0.036630 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_175-16 | 0.01056 ns   | 520,500   | 175.0                | 116.0                         | 42,582                   | 0.042582 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_200-16 | 0.01181 ns   | 580,000   | 200.0                | 133.0                         | 48,532                   | 0.048532 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_225-16 | 0.01339 ns   | 637,198   | 225.0                | 150.0                         | 54,256                   | 0.054256 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_250-16 | 0.01411 ns   | 699,020   | 250.0                | 166.0                         | 60,434                   | 0.060434 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_300-16 | 0.01931 ns   | 818,020   | 300.0                | 200.0                         | 72,334                   | 0.072334 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_400-16 | 0.02312 ns   | 1,056,020 | 400.0                | 266.0                         | 96,134                   | 0.096134 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_500-16 | 0.01675 ns   | 1,288,968 | 500.0                | 333.0                         | 119,433                  | 0.119433 MB           |

#### DeliverTx: `BenchmarkIBC_DeliverTx_Update_Client_Multi`

The benchmarks of executing `deliverTx` on a single transaction containing an IBC `updateClient` with different numbers of required signatures:

| Benchmark Name                                                          | Time (ns/op) | Gas Used  | Number of Validators | Number of Verified Signatures | Transaction Size (Bytes) | Transaction Size (MB) |
|-------------------------------------------------------------------------|--------------|-----------|----------------------|-------------------------------|--------------------------|-----------------------|
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_2-16   | 0.0006931 ns | 107,520   | 2.0                  | 1.0                           | 1,396                    | 0.001396 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_10-16  | 0.004647 ns  | 126,480   | 10.0                 | 6.0                           | 3,292                    | 0.003292 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_25-16  | 0.005861 ns  | 162,352   | 25.0                 | 16.0                          | 6,875                    | 0.006875 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_50-16  | 0.009248 ns  | 221,852   | 50.0                 | 33.0                          | 12,825                   | 0.012825 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_75-16  | 0.01252 ns   | 281,402   | 75.0                 | 50.0                          | 18,780                   | 0.018780 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_100-16 | 0.01239 ns   | 339,850   | 100.0                | 66.0                          | 24,629                   | 0.024629 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_125-16 | 0.01300 ns   | 400,402   | 125.0                | 83.0                          | 30,680                   | 0.030680 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_150-16 | 0.01691 ns   | 459,902   | 150.0                | 100.0                         | 36,630                   | 0.036630 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_175-16 | 0.01560 ns   | 517,620   | 175.0                | 116.0                         | 42,406                   | 0.042406 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_200-16 | 0.01894 ns   | 578,922   | 200.0                | 133.0                         | 48,532                   | 0.048532 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_225-16 | 0.01714 ns   | 638,422   | 225.0                | 150.0                         | 54,482                   | 0.054482 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_250-16 | 0.01736 ns   | 697,942   | 250.0                | 166.0                         | 60,434                   | 0.060434 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_300-16 | 0.02008 ns   | 816,942   | 300.0                | 200.0                         | 72,334                   | 0.072334 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_400-16 | 0.02320 ns   | 1,054,942 | 400.0                | 266.0                         | 96,134                   | 0.096134 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_500-16 | 0.02724 ns   | 1,288,522 | 500.0                | 333.0                         | 119,492                  | 0.119492 MB           |

#### PrepareProposal: `BenchmarkIBC_PrepareProposal_Update_Client_Multi`

Benchmarks of an `8mb` containing the maximum number of IBC `UpdateClient` with different number of signatures:

| Benchmark Name                                                                | Block Size (MB)  | Number of Transactions  | Number of Validators | Number of Verified Signatures | Prepare Proposal Time (s)   | Total Gas Used   | Transaction Size (Bytes)   | Transaction Size (MB)  |
|-------------------------------------------------------------------------------|------------------|-------------------------|----------------------|-------------------------------|-----------------------------|------------------|----------------------------|------------------------|
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_10-16  | 7.464            | 2,367                   | 10.0                 | 6.0                           | 0.571 s                     | 266,926,655      | 3,373                      | 0.003373 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_25-16  | 7.465            | 1,138                   | 25.0                 | 16.0                          | 0.436 s                     | 249,391,655      | 6,945                      | 0.006945 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_50-16  | 7.462            | 610.0                   | 50.0                 | 33.0                          | 0.271 s                     | 184,196,655      | 12,895                     | 0.012895 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_75-16  | 7.452            | 416.0                   | 75.0                 | 50.0                          | 0.181 s                     | 121,879,155      | 18,850                     | 0.018850 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_100-16 | 7.453            | 316.0                   | 100.0                | 66.0                          | 0.180 s                     | 151,629,155      | 24,800                     | 0.024800 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_125-16 | 7.462            | 255.0                   | 125.0                | 83.0                          | 0.197 s                     | 181,379,155      | 30,750                     | 0.030750 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_150-16 | 7.441            | 213.0                   | 150.0                | 100.0                         | 0.207 s                     | 211,129,155      | 36,700                     | 0.036700 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_175-16 | 7.432            | 183.0                   | 175.0                | 116.0                         | 0.215 s                     | 240,889,155      | 42,652                     | 0.042652 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_200-16 | 7.467            | 162.0                   | 200.0                | 133.0                         | 0.227 s                     | 269,634,155      | 48,401                     | 0.048401 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_225-16 | 7.451            | 144.0                   | 225.0                | 150.0                         | 0.235 s                     | 299,259,155      | 54,326                     | 0.054326 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_250-16 | 7.462            | 130.0                   | 250.0                | 166.0                         | 0.242 s                     | 328,894,155      | 60,253                     | 0.060253 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_300-16 | 7.450            | 108.0                   | 300.0                | 200.0                         | 0.270 s                     | 389,649,155      | 72,404                     | 0.072404 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_400-16 | 7.426            | 81.0                    | 400.0                | 266.0                         | 0.304 s                     | 508,649,155      | 96,204                     | 0.096204 MB            |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_500-16 | 7.404            | 65.0                    | 500.0                | 333.0                         | 0.361 s                     | 625,144,155      | 119,503                    | 0.119503 MB            |

#### ProcessProposal: `BenchmarkIBC_ProcessProposal_Update_Client_Multi`

Benchmarks of an `8mb` containing the maximum number of IBC `UpdateClient` with different number of signatures:

| Benchmark Name                                                                | Block Size (MB) | Number of Transactions | Number of Validators | Number of Verified Signatures | Process Proposal Time (s) | Total Gas Used | Transaction Size (Bytes) | Transaction Size (MB) |
|-------------------------------------------------------------------------------|-----------------|------------------------|----------------------|-------------------------------|---------------------------|----------------|--------------------------|-----------------------|
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_2-16   | 7.457           | 5,574                  | 2.0                  | 1.0                           | 1.022 s                   | 419,611,655    | 1,469                    | 0.001469 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_10-16  | 7.464           | 2,367                  | 10.0                 | 6.0                           | 0.455 s                   | 266,926,655    | 3,373                    | 0.003373 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_25-16  | 7.465           | 1,138                  | 25.0                 | 16.0                          | 0.270 s                   | 249,391,655    | 6,945                    | 0.006945 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_50-16  | 7.462           | 610.0                  | 50.0                 | 33.0                          | 0.181 s                   | 184,196,655    | 12,895                   | 0.012895 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_75-16  | 7.452           | 416.0                  | 75.0                 | 50.0                          | 0.150 s                   | 121,879,155    | 18,850                   | 0.018850 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_100-16 | 7.453           | 316.0                  | 100.0                | 66.0                          | 0.132 s                   | 151,629,155    | 24,800                   | 0.024800 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_125-16 | 7.462           | 255.0                  | 125.0                | 83.0                          | 0.122 s                   | 181,379,155    | 30,750                   | 0.030750 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_150-16 | 7.441           | 213.0                  | 150.0                | 100.0                         | 0.107 s                   | 211,129,155    | 36,700                   | 0.036700 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_175-16 | 7.442           | 184.0                  | 175.0                | 116.0                         | 0.092 s                   | 240,009,155    | 42,476                   | 0.042476 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_200-16 | 7.452           | 161.0                  | 200.0                | 133.0                         | 0.098 s                   | 270,639,155    | 48,602                   | 0.048602 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_225-16 | 7.430           | 143.0                  | 225.0                | 150.0                         | 0.089 s                   | 300,389,155    | 54,552                   | 0.054552 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_250-16 | 7.435           | 129.0                  | 250.0                | 166.0                         | 0.081 s                   | 330,149,155    | 60,504                   | 0.060504 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_300-16 | 7.450           | 108.0                  | 300.0                | 200.0                         | 0.078 s                   | 389,649,155    | 72,404                   | 0.072404 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_400-16 | 7.426           | 81.0                   | 400.0                | 266.0                         | 0.077 s                   | 508,649,155    | 96,204                   | 0.096204 MB           |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_500-16 | 7.435           | 65.0                   | 500.0                | 333.0                         | 0.092 s                   | 627,649,155    | 120,004                  | 0.120004 MB           |

#### Process proposal time with different number of transactions per block

**50 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1467                   | 0.532979           | 0.2508           |
| 1546                   | 0.561684           | 0.2766           |
| 1566                   | 0.568951           | 0.2511           |
| 1650                   | 0.599472           | 0.2711           |
| 1739                   | 0.631810           | 0.3007           |
| 1761                   | 0.639804           | 0.2832           |
| 1856                   | 0.674322           | 0.3017           |
| 1956                   | 0.710657           | 0.3203           |
| 1980                   | 0.719378           | 0.3291           |
| 2062                   | 0.749172           | 0.3670           |
| 2088                   | 0.758619           | 0.3426           |
| 2200                   | 0.799314           | 0.3610           |
| 2319                   | 0.842553           | 0.3980           |
| 2349                   | 0.853454           | 0.3794           |
| 2475                   | 0.899236           | 0.4086           |
| 2608                   | 0.947561           | 0.4555           |
| 2641                   | 0.959552           | 0.4561           |
| 2750                   | 0.999157           | 0.4920           |
| 2784                   | 1.011511           | 0.4782           |
| 2934                   | 1.066013           | 0.5209           |
| 2970                   | 1.079094           | 0.5069           |
| 3093                   | 1.123786           | 0.5816           |
| 3132                   | 1.137957           | 0.5360           |
| 3300                   | 1.198999           | 0.5766           |
| 3478                   | 1.263676           | 0.6072           |
| 3523                   | 1.280026           | 0.6028           |
| 3712                   | 1.348700           | 0.6394           |
| 3912                   | 1.421370           | 0.6928           |
| 3961                   | 1.439174           | 0.6559           |
| 4125                   | 1.498763           | 0.7463           |
| 4176                   | 1.517294           | 0.6967           |
| 5500                   | 1.998369           | 0.9183           |
| 11000                  | 3.753713           | 1.732            |

**100 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1546                   | 0.636877           | 0.2726           |
| 1739                   | 0.716391           | 0.2762           |
| 1956                   | 0.805792           | 0.3207           |
| 2062                   | 0.849463           | 0.3361           |
| 2319                   | 0.955343           | 0.3774           |
| 2608                   | 1.074408           | 0.4387           |
| 2750                   | 1.132910           | 0.4873           |
| 2934                   | 1.208715           | 0.5015           |
| 3093                   | 1.274221           | 0.5202           |
| 3478                   | 1.432837           | 0.5797           |
| 3912                   | 1.611639           | 0.6520           |
| 4125                   | 1.699392           | 0.6758           |
| 5500                   | 2.265875           | 0.9318           |
| 11000                  | 4.256186           | 1.685            |

**200 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1546                   | 0.787264           | 0.2472           |
| 1739                   | 0.885551           | 0.3009           |
| 1956                   | 0.996061           | 0.3188           |
| 2062                   | 1.050043           | 0.3400           |
| 2319                   | 1.180923           | 0.3781           |
| 2608                   | 1.328100           | 0.4439           |
| 2750                   | 1.400415           | 0.4720           |
| 2934                   | 1.494120           | 0.5049           |
| 3093                   | 1.575092           | 0.5384           |
| 3478                   | 1.771158           | 0.5913           |
| 3912                   | 1.992178           | 0.6459           |
| 4125                   | 2.100651           | 0.6927           |
| 5500                   | 2.800886           | 0.8970           |
| 11000                  | 5.254511           | 1.691            |

**300 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1546                   | 0.934702           | 0.2506           |
| 1739                   | 1.051395           | 0.2910           |
| 1956                   | 1.182600           | 0.3316           |
| 2062                   | 1.246691           | 0.3439           |
| 2319                   | 1.402081           | 0.3830           |
| 2608                   | 1.576818           | 0.4674           |
| 2750                   | 1.662676           | 0.4803           |
| 2934                   | 1.773928           | 0.5110           |
| 3093                   | 1.870064           | 0.5431           |
| 3478                   | 2.102846           | 0.6002           |
| 3912                   | 2.365255           | 0.6659           |
| 4125                   | 2.494041           | 0.7052           |
| 5500                   | 3.325407           | 0.9117           |
| 11000                  | 6.238512           | 1.688            |

**400 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1375                   | 0.962440           | 0.2425           |
| 1467                   | 1.026840           | 0.2564           |
| 1546                   | 1.082140           | 0.2583           |
| 1650                   | 1.154940           | 0.2713           |
| 1739                   | 1.217239           | 0.2854           |
| 1856                   | 1.299139           | 0.3204           |
| 1956                   | 1.369139           | 0.3205           |
| 2062                   | 1.443338           | 0.3535           |
| 2200                   | 1.539938           | 0.3674           |
| 2319                   | 1.623238           | 0.3873           |
| 2475                   | 1.732437           | 0.4184           |
| 2608                   | 1.825537           | 0.4635           |
| 2750                   | 1.924936           | 0.5227           |
| 2784                   | 1.948736           | 0.5029           |
| 2934                   | 2.053736           | 0.5193           |
| 3093                   | 2.165035           | 0.5505           |
| 3300                   | 2.309935           | 0.6121           |
| 3478                   | 2.434534           | 0.6077           |
| 3712                   | 2.598333           | 0.6534           |
| 3912                   | 2.738333           | 0.6625           |
| 5500                   | 3.849928           | 0.9410           |
| 11000                  | 7.222513           | 1.782            |

**500 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1476                   | 1.173903           | 0.2640           |
| 1660                   | 1.320250           | 0.3192           |
| 1750                   | 1.391832           | 0.3249           |
| 1867                   | 1.484890           | 0.3494           |
| 1968                   | 1.565222           | 0.3664           |
| 2214                   | 1.760881           | 0.4322           |
| 2490                   | 1.980402           | 0.4667           |
| 2625                   | 2.087776           | 0.4795           |
| 2800                   | 2.226965           | 0.5033           |
| 2952                   | 2.347860           | 0.5529           |
| 3321                   | 2.641350           | 0.6263           |
| 3500                   | 2.783720           | 0.6101           |
| 3735                   | 2.970631           | 0.6629           |
| 3937                   | 3.131294           | 0.7341           |
| 7000                   | 5.035397           | 1.127            |

**600 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1400                   | 1.246969           | 0.2492           |
| 1417                   | 1.262112           | 0.2554           |
| 1432                   | 1.275473           | 0.2465           |
| 1476                   | 1.314665           | 0.2575           |
| 1494                   | 1.330698           | 0.2716           |
| 1510                   | 1.344950           | 0.2729           |
| 1575                   | 1.402847           | 0.2777           |
| 1593                   | 1.418880           | 0.3210           |
| 1611                   | 1.434914           | 0.3269           |
| 1660                   | 1.478559           | 0.3331           |
| 1680                   | 1.496374           | 0.3202           |
| 1698                   | 1.512407           | 0.3387           |
| 1750                   | 1.558725           | 0.3430           |
| 1771                   | 1.577431           | 0.3476           |
| 1791                   | 1.595245           | 0.3550           |
| 1812                   | 1.613951           | 0.3526           |
| 1867                   | 1.662941           | 0.3702           |
| 1890                   | 1.683428           | 0.3592           |
| 1910                   | 1.701242           | 0.3728           |
| 1968                   | 1.752905           | 0.3790           |
| 1992                   | 1.774282           | 0.3636           |
| 2014                   | 1.793879           | 0.3740           |
| 2100                   | 1.870481           | 0.4125           |
| 2125                   | 1.892750           | 0.3915           |
| 2148                   | 1.913237           | 0.4158           |
| 2214                   | 1.972025           | 0.4057           |
| 2241                   | 1.996075           | 0.4231           |
| 2265                   | 2.017452           | 0.4210           |
| 2362                   | 2.103853           | 0.4392           |
| 2389                   | 2.127903           | 0.4406           |
| 2416                   | 2.151953           | 0.4700           |
| 2490                   | 2.217867           | 0.4615           |
| 2520                   | 2.244589           | 0.4727           |
| 2547                   | 2.268639           | 0.4743           |
| 2625                   | 2.338116           | 0.4812           |
| 2656                   | 2.365728           | 0.4923           |
| 2686                   | 2.392450           | 0.4905           |
| 2718                   | 2.420954           | 0.5042           |
| 2800                   | 2.493994           | 0.5309           |
| 2835                   | 2.525169           | 0.5166           |
| 2865                   | 2.551891           | 0.5340           |
| 2952                   | 2.629385           | 0.5378           |
| 2988                   | 2.661451           | 0.5504           |
| 3021                   | 2.690845           | 0.5532           |
| 3150                   | 2.805750           | 0.5948           |
| 3187                   | 2.838707           | 0.5747           |
| 3222                   | 2.869883           | 0.5986           |
| 3321                   | 2.958065           | 0.6170           |
| 3361                   | 2.993694           | 0.6092           |
| 3397                   | 3.025761           | 0.6193           |
| 3500                   | 3.117506           | 0.6357           |
| 3543                   | 3.155807           | 0.6425           |
| 3583                   | 3.191437           | 0.6764           |
| 3624                   | 3.227957           | 0.6628           |
| 3735                   | 3.326828           | 0.6819           |
| 3780                   | 3.366911           | 0.6935           |
| 3820                   | 3.402540           | 0.7127           |
| 3937                   | 3.506756           | 0.7093           |
| 3984                   | 3.548620           | 0.7404           |
| 4029                   | 3.588703           | 0.7535           |
| 7000                   | 5.639168           | 1.133            |

**1000 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1333                   | 1.695789           | 0.2682           |
| 1348                   | 1.714872           | 0.2605           |
| 1406                   | 1.788660           | 0.2858           |
| 1422                   | 1.809015           | 0.2827           |
| 1437                   | 1.828098           | 0.2881           |
| 1499                   | 1.906975           | 0.2945           |
| 1516                   | 1.928602           | 0.2985           |
| 1581                   | 2.011295           | 0.3039           |
| 1599                   | 2.034195           | 0.3111           |
| 1616                   | 2.055822           | 0.3185           |
| 1686                   | 2.144876           | 0.3450           |
| 1705                   | 2.169048           | 0.3501           |
| 1778                   | 2.261919           | 0.3496           |
| 1798                   | 2.287363           | 0.3554           |
| 1818                   | 2.312807           | 0.3507           |
| 1875                   | 2.385323           | 0.3849           |
| 1896                   | 2.412039           | 0.3877           |
| 1917                   | 2.438755           | 0.3746           |
| 1999                   | 2.543076           | 0.3815           |
| 2022                   | 2.572336           | 0.4042           |
| 2109                   | 2.683018           | 0.4223           |
| 2133                   | 2.713551           | 0.4126           |
| 2155                   | 2.741539           | 0.4115           |
| 2248                   | 2.859854           | 0.4183           |
| 2274                   | 2.892931           | 0.4343           |
| 2371                   | 3.016335           | 0.4642           |
| 2398                   | 3.050684           | 0.4631           |
| 2424                   | 3.083761           | 0.4575           |
| 2500                   | 3.180449           | 0.4825           |
| 2529                   | 3.217342           | 0.4757           |
| 2557                   | 3.252964           | 0.4812           |
| 2667                   | 3.392906           | 0.5144           |
| 2697                   | 3.431072           | 0.5141           |
| 2727                   | 3.469238           | 0.5071           |
| 2812                   | 3.577375           | 0.5250           |
| 2844                   | 3.618086           | 0.5359           |
| 2875                   | 3.657524           | 0.5506           |
| 2998                   | 3.814005           | 0.5659           |
| 3033                   | 3.858532           | 0.5797           |
| 3163                   | 4.023918           | 0.5964           |
| 3199                   | 4.069717           | 0.6023           |
| 3232                   | 4.111700           | 0.6142           |
| 3372                   | 4.289808           | 0.6249           |
| 3411                   | 4.339424           | 0.6465           |
| 3556                   | 4.523893           | 0.6488           |
| 3597                   | 4.576054           | 0.6829           |
| 3636                   | 4.625669           | 0.6699           |
| 3750                   | 4.770700           | 0.6820           |
| 3793                   | 4.825405           | 0.6983           |
| 3835                   | 4.878838           | 0.6991           |
| 5000                   | 5.808817           | 0.8490           |

**1200 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1406                   | 2.056833           | 0.2758           |
| 1500                   | 2.194349           | 0.3071           |
| 1581                   | 2.312847           | 0.3054           |
| 1687                   | 2.467918           | 0.3332           |
| 1778                   | 2.601046           | 0.3569           |
| 1875                   | 2.742950           | 0.3688           |
| 2000                   | 2.925817           | 0.3793           |
| 2109                   | 3.085278           | 0.4087           |
| 2250                   | 3.291552           | 0.4359           |
| 2371                   | 3.468567           | 0.4462           |
| 2500                   | 3.657286           | 0.4789           |
| 2530                   | 3.701174           | 0.4999           |
| 2667                   | 3.901596           | 0.4836           |
| 2812                   | 4.113722           | 0.5371           |
| 3000                   | 4.388754           | 0.5768           |
| 3163                   | 4.627213           | 0.5897           |
| 3375                   | 4.937355           | 0.6156           |
| 3556                   | 5.202147           | 0.6549           |
| 3750                   | 5.485956           | 0.6933           |
| 4000                   | 5.851690           | 0.7415           |
| 5000                   | 6.679712           | 0.8498           |

**1500 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1406                   | 2.459093           | 0.2941           |
| 1581                   | 2.765175           | 0.3109           |
| 1778                   | 3.109735           | 0.3373           |
| 1875                   | 3.279392           | 0.3706           |
| 2109                   | 3.688667           | 0.4100           |
| 2371                   | 4.146915           | 0.4601           |
| 2500                   | 4.372541           | 0.4735           |
| 2667                   | 4.664631           | 0.5013           |
| 2812                   | 4.918242           | 0.5260           |
| 3163                   | 5.532154           | 0.5946           |
| 3556                   | 6.219526           | 0.6634           |
| 3750                   | 6.245762           | 0.6879           |
| 5000                   | 6.245762           | 0.6781           |

**1800 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1333                   | 2.712788           | 0.2643           |
| 1406                   | 2.861353           | 0.2840           |
| 1422                   | 2.893915           | 0.2843           |
| 1499                   | 3.050621           | 0.2956           |
| 1581                   | 3.217503           | 0.3094           |
| 1599                   | 3.254135           | 0.3302           |
| 1686                   | 3.431192           | 0.3396           |
| 1778                   | 3.618425           | 0.3407           |
| 1798                   | 3.659128           | 0.3397           |
| 1875                   | 3.815834           | 0.3777           |
| 1896                   | 3.858572           | 0.3813           |
| 1999                   | 4.068192           | 0.3647           |
| 2109                   | 4.292057           | 0.4191           |
| 2133                   | 4.340900           | 0.4057           |
| 2248                   | 4.574942           | 0.4349           |
| 2371                   | 4.825264           | 0.4446           |
| 2398                   | 4.880213           | 0.4481           |
| 2500                   | 5.087797           | 0.4676           |
| 2529                   | 5.146816           | 0.4740           |
| 2667                   | 5.427666           | 0.5127           |
| 2697                   | 5.488720           | 0.5039           |
| 2812                   | 5.722761           | 0.5547           |
| 2844                   | 5.787886           | 0.5411           |
| 2998                   | 6.101297           | 0.5710           |
| 3163                   | 6.437096           | 0.5896           |
| 3199                   | 6.510361           | 0.5965           |
| 3372                   | 6.862440           | 0.6149           |
| 3556                   | 7.236906           | 0.6572           |
| 3597                   | 7.267433           | 0.6716           |
| 5000                   | 7.267433           | 0.6742           |

**2000 bytes blobs**:

| Number of Transactions | Block Size (bytes) | Elapsed Time (s) |
|------------------------|--------------------|------------------|
| 1406                   | 3.129526           | 0.2732           |
| 1581                   | 3.519054           | 0.3078           |
| 1778                   | 3.957552           | 0.3477           |
| 1875                   | 4.173462           | 0.3764           |
| 2109                   | 4.694317           | 0.4059           |
| 2371                   | 5.277496           | 0.4412           |
| 2500                   | 5.564634           | 0.4664           |
| 2667                   | 5.936356           | 0.5006           |
| 2812                   | 6.259108           | 0.5262           |
| 3163                   | 6.526213           | 0.5574           |
| 3556                   | 6.526213           | 0.5667           |
| 3750                   | 6.526213           | 0.5509           |
| 5000                   | 6.526213           | 0.5556           |

## GoLevelDB benchmarks

### `sendMsg` benchmarks

#### CheckTx

A single `checkTx` of a `sendMsg` message takes 0.0003071 **ns** to execute. And it uses 74374 gas.

The transactions in an `8mb` block containing 31645 `sendMsg` messages take 6,45 s (6455816060 ns) to run `checkTx` on all of them. The total gas used is 1884371034 gas.

#### DeliverTx

A single `deliverTx` of a `sendMsg` message takes 0.0003948 **ns** to execute. And it uses 103251 gas.

The transactions in an `8mb` block containing 31645 `sendMsg` messages take 7,50 s (7506830940 ns) to run `deliverTx` on all of them. The total gas used is 2801272121 gas.

#### PrepareProposal

A single `prepareProposal` of a `sendMsg` message takes 0.0003943 **ns** to execute. And it uses 101110 gas.

An `8mb` block containing 31645 `sendMsg` messages takes 5,2 s (5242159792 ns) to execute. The total gas used 1843040790 gas.

#### ProcessProposal

A single `processProposal` of a `sendMsg` message takes 0.0003010 **ns** to execute. And it uses 101110 gas.

An `8mb` block containing 31645 `sendMsg` messages takes 5,21 s (5214205041 ns) to execute. The total gas used 1843040790 gas.

### `PFB` benchmarks

#### CheckTx: `BenchmarkCheckTx_PFB_Multi`

Benchmarks of `CheckTx` for a single PFB with different sizes:

| Benchmark Name                              | Time (ns/op) | Gas Used | Transaction Size (Bytes) | Transaction Size (MB) |
|---------------------------------------------|--------------|----------|--------------------------|-----------------------|
| BenchmarkCheckTx_PFB_Multi/300_bytes-16     | 0.0005847 ns | 74,664   | 703                      | 0.000703 MB           |
| BenchmarkCheckTx_PFB_Multi/500_bytes-16     | 0.0005136 ns | 74,664   | 903                      | 0.000903 MB           |
| BenchmarkCheckTx_PFB_Multi/1000_bytes-16    | 0.0005754 ns | 74,664   | 1,403                    | 0.001403 MB           |
| BenchmarkCheckTx_PFB_Multi/5000_bytes-16    | 0.0005706 ns | 74,664   | 5,403                    | 0.005403 MB           |
| BenchmarkCheckTx_PFB_Multi/10000_bytes-16   | 0.0006885 ns | 74,664   | 10,403                   | 0.010403 MB           |
| BenchmarkCheckTx_PFB_Multi/50000_bytes-16   | 0.0006683 ns | 74,674   | 50,406                   | 0.050406 MB           |
| BenchmarkCheckTx_PFB_Multi/100000_bytes-16  | 0.0008378 ns | 74,674   | 100,406                  | 0.100406 MB           |
| BenchmarkCheckTx_PFB_Multi/200000_bytes-16  | 0.001130 ns  | 74,674   | 200,406                  | 0.200406 MB           |
| BenchmarkCheckTx_PFB_Multi/300000_bytes-16  | 0.001164 ns  | 74,674   | 300,406                  | 0.300406 MB           |
| BenchmarkCheckTx_PFB_Multi/400000_bytes-16  | 0.001550 ns  | 74,674   | 400,406                  | 0.400406 MB           |
| BenchmarkCheckTx_PFB_Multi/500000_bytes-16  | 0.001829 ns  | 74,674   | 500,406                  | 0.500406 MB           |
| BenchmarkCheckTx_PFB_Multi/1000000_bytes-16 | 0.002452 ns  | 74,674   | 1,000,406                | 1.000406 MB           |
| BenchmarkCheckTx_PFB_Multi/2000000_bytes-16 | 0.004647 ns  | 74,674   | 2,000,406                | 2.000406 MB           |
| BenchmarkCheckTx_PFB_Multi/3000000_bytes-16 | 0.006415 ns  | 74,684   | 3,000,409                | 3.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/4000000_bytes-16 | 0.007709 ns  | 74,684   | 4,000,409                | 4.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/5000000_bytes-16 | 0.01014 ns   | 74,684   | 5,000,409                | 5.000409 MB           |
| BenchmarkCheckTx_PFB_Multi/6000000_bytes-16 | 0.01153 ns   | 74,684   | 6,000,409                | 6.000409 MB           |

#### DeliverTx: `BenchmarkDeliverTx_PFB_Multi`

Benchmarks of `DeliverTx` for a single PFB with different sizes:

| Benchmark Name                                | Time (ns/op) | Gas Used   | Transaction Size (Bytes) | Transaction Size (MB) |
|-----------------------------------------------|--------------|------------|--------------------------|-----------------------|
| BenchmarkDeliverTx_PFB_Multi/300_bytes-16     | 0.0005010 ns | 77,682     | 703                      | 0.000703 MB           |
| BenchmarkDeliverTx_PFB_Multi/500_bytes-16     | 0.0004297 ns | 81,778     | 903                      | 0.000903 MB           |
| BenchmarkDeliverTx_PFB_Multi/1000_bytes-16    | 0.0005227 ns | 85,874     | 1,403                    | 0.001403 MB           |
| BenchmarkDeliverTx_PFB_Multi/5000_bytes-16    | 0.0005552 ns | 118,642    | 5,403                    | 0.005403 MB           |
| BenchmarkDeliverTx_PFB_Multi/10000_bytes-16   | 0.0004537 ns | 159,602    | 10,403                   | 0.010403 MB           |
| BenchmarkDeliverTx_PFB_Multi/50000_bytes-16   | 0.0004896 ns | 499,580    | 50,406                   | 0.050406 MB           |
| BenchmarkDeliverTx_PFB_Multi/100000_bytes-16  | 0.0005505 ns | 925,564    | 100,406                  | 0.100406 MB           |
| BenchmarkDeliverTx_PFB_Multi/200000_bytes-16  | 0.0003661 ns | 1,773,436  | 200,406                  | 0.200406 MB           |
| BenchmarkDeliverTx_PFB_Multi/300000_bytes-16  | 0.0004681 ns | 2,625,404  | 300,406                  | 0.300406 MB           |
| BenchmarkDeliverTx_PFB_Multi/400000_bytes-16  | 0.0003012 ns | 3,473,276  | 400,406                  | 0.400406 MB           |
| BenchmarkDeliverTx_PFB_Multi/500000_bytes-16  | 0.0003164 ns | 4,325,244  | 500,406                  | 0.500406 MB           |
| BenchmarkDeliverTx_PFB_Multi/1000000_bytes-16 | 0.0004873 ns | 8,572,796  | 1,000,406                | 1.000406 MB           |
| BenchmarkDeliverTx_PFB_Multi/2000000_bytes-16 | 0.0004004 ns | 17,071,996 | 2,000,406                | 2.000406 MB           |
| BenchmarkDeliverTx_PFB_Multi/3000000_bytes-16 | 0.0003486 ns | 25,571,206 | 3,000,409                | 3.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/4000000_bytes-16 | 0.0004354 ns | 34,066,310 | 4,000,409                | 4.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/5000000_bytes-16 | 0.0003734 ns | 42,565,510 | 5,000,409                | 5.000409 MB           |
| BenchmarkDeliverTx_PFB_Multi/6000000_bytes-16 | 0.0003595 ns | 51,064,710 | 6,000,409                | 6.000409 MB           |

#### PrepareProposal: `BenchmarkPrepareProposal_PFB_Multi`

The benchmarks for `PrepareProposal` for 8mb blocks containing PFBs of different sizes:

| Benchmark Name                                                         | Block Size (MB) | Number of Transactions | Prepare Proposal Time (s) | Total Gas Used  | Transaction Size (Bytes) | Transaction Size (MB) |
|------------------------------------------------------------------------|-----------------|------------------------|---------------------------|-----------------|--------------------------|-----------------------|
| BenchmarkPrepareProposal_PFB_Multi/15000_transactions_of_300_bytes-16  | 6.239           | 10,318                 | 2.452 s                   | 988,490,895,000 | 703                      | 0.000703 MB           |
| BenchmarkPrepareProposal_PFB_Multi/10000_transactions_of_500_bytes-16  | 5.035           | 6,331                  | 1.721 s                   | 439,343,930,000 | 903                      | 0.000903 MB           |
| BenchmarkPrepareProposal_PFB_Multi/6000_transactions_of_1000_bytes-16  | 5.809           | 4,566                  | 1.063 s                   | 158,174,358,000 | 1,403                    | 0.001403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3000_transactions_of_5000_bytes-16  | 7.188           | 1,413                  | 0.527 s                   | 39,550,179,000  | 5,403                    | 0.005403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/1000_transactions_of_10000_bytes-16 | 7.470           | 758                    | 0.210 s                   | 4,397,393,000   | 10,403                   | 0.010403 MB           |
| BenchmarkPrepareProposal_PFB_Multi/500_transactions_of_50000_bytes-16  | 7.441           | 155                    | 0.125 s                   | 1,100,446,500   | 50,406                   | 0.050406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/100_transactions_of_100000_bytes-16 | 7.368           | 77                     | 0.061 s                   | 44,369,300      | 100,406                  | 0.100406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/100_transactions_of_200000_bytes-16 | 7.260           | 38                     | 0.058 s                   | 44,369,300      | 200,406                  | 0.200406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/50_transactions_of_300000_bytes-16  | 7.161           | 25                     | 0.042 s                   | 11,202,150      | 300,406                  | 0.300406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/50_transactions_of_400000_bytes-16  | 7.254           | 19                     | 0.038 s                   | 11,202,150      | 400,406                  | 0.400406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/30_transactions_of_500000_bytes-16  | 7.157           | 15                     | 0.031 s                   | 4,085,490       | 500,406                  | 0.500406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/10_transactions_of_1000000_bytes-16 | 6.678           | 7                      | 0.026 s                   | 483,230         | 1,000,406                | 1.000406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/5_transactions_of_2000000_bytes-16  | 5.723           | 3                      | 0.027 s                   | 131,790         | 2,000,406                | 2.000406 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3_transactions_of_3000000_bytes-16  | 5.723           | 2                      | 0.030 s                   | 52,716          | 3,000,409                | 3.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/3_transactions_of_4000000_bytes-16  | 3.815           | 1                      | 0.026 s                   | 52,716          | 4,000,409                | 4.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/2_transactions_of_5000000_bytes-16  | 4.769           | 1                      | 0.031 s                   | 26,358          | 5,000,409                | 5.000409 MB           |
| BenchmarkPrepareProposal_PFB_Multi/2_transactions_of_6000000_bytes-16  | 5.722           | 1                      | 0.028 s                   | 26,358          | 6,000,409                | 6.000409 MB           |

#### ProcessProposal: `BenchmarkProcessProposal_PFB_Multi`

The benchmarks for `ProcessProposal` for 8mb blocks containing PFBs of different sizes:

| Benchmark Name                                                         | Block Size (MB) | Number of Transactions | Process Proposal Time (s) | Total Gas Used  | Transaction Size (Bytes) | Transaction Size (MB) |
|------------------------------------------------------------------------|-----------------|------------------------|---------------------------|-----------------|--------------------------|-----------------------|
| BenchmarkProcessProposal_PFB_Multi/15000_transactions_of_300_bytes-16  | 6.239           | 10,318                 | 1.813 s                   | 988,490,895,000 | 703                      | 0.000703 MB           |
| BenchmarkProcessProposal_PFB_Multi/10000_transactions_of_500_bytes-16  | 5.035           | 6,331                  | 1.120 s                   | 439,343,930,000 | 903                      | 0.000903 MB           |
| BenchmarkProcessProposal_PFB_Multi/6000_transactions_of_1000_bytes-16  | 5.809           | 4,566                  | 0.829 s                   | 158,174,358,000 | 1,403                    | 0.001403 MB           |
| BenchmarkProcessProposal_PFB_Multi/3000_transactions_of_5000_bytes-16  | 7.188           | 1,413                  | 0.290 s                   | 39,550,179,000  | 5,403                    | 0.005403 MB           |
| BenchmarkProcessProposal_PFB_Multi/1000_transactions_of_10000_bytes-16 | 7.470           | 758                    | 0.188 s                   | 4,397,393,000   | 10,403                   | 0.010403 MB           |
| BenchmarkProcessProposal_PFB_Multi/500_transactions_of_50000_bytes-16  | 7.441           | 155                    | 0.076 s                   | 1,100,446,500   | 50,406                   | 0.050406 MB           |
| BenchmarkProcessProposal_PFB_Multi/100_transactions_of_100000_bytes-16 | 7.368           | 77                     | 0.056 s                   | 44,369,300      | 100,406                  | 0.100406 MB           |
| BenchmarkProcessProposal_PFB_Multi/100_transactions_of_200000_bytes-16 | 7.260           | 38                     | 0.050 s                   | 44,369,300      | 200,406                  | 0.200406 MB           |
| BenchmarkProcessProposal_PFB_Multi/50_transactions_of_300000_bytes-16  | 7.161           | 25                     | 0.048 s                   | 11,202,150      | 300,406                  | 0.300406 MB           |
| BenchmarkProcessProposal_PFB_Multi/50_transactions_of_400000_bytes-16  | 7.254           | 19                     | 0.048 s                   | 11,202,150      | 400,406                  | 0.400406 MB           |
| BenchmarkProcessProposal_PFB_Multi/30_transactions_of_500000_bytes-16  | 7.157           | 15                     | 0.043 s                   | 4,085,490       | 500,406                  | 0.500406 MB           |
| BenchmarkProcessProposal_PFB_Multi/10_transactions_of_1000000_bytes-16 | 6.678           | 7                      | 0.041 s                   | 483,230         | 1,000,406                | 1.000406 MB           |
| BenchmarkProcessProposal_PFB_Multi/5_transactions_of_2000000_bytes-16  | 5.723           | 3                      | 0.053 s                   | 131,790         | 2,000,406                | 2.000406 MB           |
| BenchmarkProcessProposal_PFB_Multi/3_transactions_of_3000000_bytes-16  | 5.723           | 2                      | 0.037 s                   | 52,716          | 3,000,409                | 3.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/3_transactions_of_4000000_bytes-16  | 3.815           | 1                      | 0.071 s                   | 52,716          | 4,000,409                | 4.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/2_transactions_of_5000000_bytes-16  | 4.769           | 1                      | 0.034 s                   | 26,358          | 5,000,409                | 5.000409 MB           |
| BenchmarkProcessProposal_PFB_Multi/2_transactions_of_6000000_bytes-16  | 5.722           | 1                      | 0.062 s                   | 26,358          | 6,000,409                | 6.000409 MB           |

### IBC `UpdateClient` benchmarks

#### CheckTx: `BenchmarkIBC_CheckTx_Update_Client_Multi`

The benchmarks of executing `checkTx` on a single transaction containing an IBC `updateClient` with different numbers of required signatures:

| Benchmark Name                                                        | Time (ns/op) | Total Gas Used | Number of Validators | Number of Verified Signatures | Transaction Size (Bytes) | Transaction Size (MB) |
|-----------------------------------------------------------------------|--------------|----------------|----------------------|-------------------------------|--------------------------|-----------------------|
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_2-16   | 1,370        | 108,670        | 2                    | 1                             | 1,399                    | 0.001399 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_10-16  | 3,577        | 127,710        | 10                   | 6                             | 3,303                    | 0.003303 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_25-16  | 7,432        | 163,430        | 25                   | 16                            | 6,875                    | 0.006875 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_50-16  | 9,879        | 222,930        | 50                   | 33                            | 12,825                   | 0.012825 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_75-16  | 12,060       | 282,480        | 75                   | 50                            | 18,780                   | 0.018780 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_100-16 | 13,080       | 341,980        | 100                  | 66                            | 24,730                   | 0.024730 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_125-16 | 14,390       | 401,480        | 125                  | 83                            | 30,680                   | 0.030680 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_150-16 | 16,440       | 459,428        | 150                  | 100                           | 36,479                   | 0.036479 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_175-16 | 17,370       | 520,500        | 175                  | 116                           | 42,582                   | 0.042582 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_200-16 | 18,840       | 580,000        | 200                  | 133                           | 48,532                   | 0.048532 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_225-16 | 21,760       | 637,198        | 225                  | 150                           | 54,256                   | 0.054256 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_250-16 | 19,680       | 699,020        | 250                  | 166                           | 60,434                   | 0.060434 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_300-16 | 22,580       | 818,020        | 300                  | 200                           | 72,334                   | 0.072334 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_400-16 | 25,990       | 1,056,020      | 400                  | 266                           | 96,134                   | 0.096134 MB           |
| BenchmarkIBC_CheckTx_Update_Client_Multi/number_of_validators:_500-16 | 27,100       | 1,288,968      | 500                  | 333                           | 119,433                  | 0.119433 MB           |

#### DeliverTx: `BenchmarkIBC_DeliverTx_Update_Client_Multi`

The benchmarks of executing `deliverTx` on a single transaction containing an IBC `updateClient` with different numbers of required signatures:

| Benchmark Name                                                          | Time (ns/op) | Gas Used  | Number of Validators | Number of Verified Signatures | Transaction Size (Bytes) | Transaction Size (MB) |
|-------------------------------------------------------------------------|--------------|-----------|----------------------|-------------------------------|--------------------------|-----------------------|
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_2-16   | 1,575        | 107,592   | 2                    | 1                             | 1,399                    | 0.001399 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_10-16  | 1,240        | 126,632   | 10                   | 6                             | 3,303                    | 0.003303 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_25-16  | 1,142        | 162,352   | 25                   | 16                            | 6,875                    | 0.006875 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_50-16  | 16,260       | 221,852   | 50                   | 33                            | 12,825                   | 0.012825 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_75-16  | 13,120       | 281,402   | 75                   | 50                            | 18,780                   | 0.018780 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_100-16 | 7,336        | 340,902   | 100                  | 66                            | 24,730                   | 0.024730 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_125-16 | 7,668        | 399,100   | 125                  | 83                            | 30,554                   | 0.030554 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_150-16 | 5,603        | 459,902   | 150                  | 100                           | 36,630                   | 0.036630 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_175-16 | 11,050       | 519,422   | 175                  | 116                           | 42,582                   | 0.042582 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_200-16 | 9,553        | 578,922   | 200                  | 133                           | 48,532                   | 0.048532 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_225-16 | 13,170       | 638,422   | 225                  | 150                           | 54,482                   | 0.054482 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_250-16 | 8,286        | 695,390   | 250                  | 166                           | 60,183                   | 0.060183 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_300-16 | 15,820       | 816,942   | 300                  | 200                           | 72,334                   | 0.072334 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_400-16 | 19,650       | 1,050,890 | 400                  | 266                           | 95,733                   | 0.095733 MB           |
| BenchmarkIBC_DeliverTx_Update_Client_Multi/number_of_validators:_500-16 | 22,900       | 1,292,942 | 500                  | 333                           | 119,934                  | 0.119934 MB           |

#### PrepareProposal: `BenchmarkIBC_PrepareProposal_Update_Client_Multi`

Benchmarks of an `8mb` containing the maximum number of IBC `UpdateClient` with different number of signatures:

| Benchmark Name                                                                | Block Size (MB) | Number of Transactions | Number of Validators | Number of Verified Signatures | Prepare Proposal Time (s) | Total Gas Used | Transaction Size (Bytes) | Transaction Size (MB) |
|-------------------------------------------------------------------------------|-----------------|------------------------|----------------------|-------------------------------|---------------------------|----------------|--------------------------|-----------------------|
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_2-16   | 7.457           | 5,574                  | 2                    | 1                             | 1.0729                    | 389,819,345    | 1,469                    | 0.001469              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_10-16  | 7.464           | 2,367                  | 10                   | 6                             | 0.5564                    | 210,605,480    | 3,373                    | 0.003373              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_25-16  | 7.462           | 1,142                  | 25                   | 16                            | 0.4047                    | 142,106,425    | 6,919                    | 0.006919              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_50-16  | 7.462           | 610                    | 50                   | 33                            | 0.2432                    | 112,364,505    | 12,895                   | 0.012895              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_75-16  | 7.452           | 416                    | 75                   | 50                            | 0.1357                    | 101,405,415    | 18,850                   | 0.018850              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_100-16 | 7.453           | 316                    | 100                  | 66                            | 0.1573                    | 95,833,915     | 24,800                   | 0.024800              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_125-16 | 7.460           | 256                    | 125                  | 83                            | 0.1653                    | 92,549,255     | 30,624                   | 0.030624              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_150-16 | 7.445           | 214                    | 150                  | 100                           | 0.1804                    | 90,046,805     | 36,549                   | 0.036549              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_175-16 | 7.432           | 183                    | 175                  | 116                           | 0.1916                    | 88,172,820     | 42,652                   | 0.042652              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_200-16 | 7.452           | 161                    | 200                  | 133                           | 0.2167                    | 87,153,710     | 48,602                   | 0.048602              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_225-16 | 7.430           | 143                    | 225                  | 150                           | 0.2065                    | 85,919,620     | 54,552                   | 0.054552              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_250-16 | 7.435           | 129                    | 250                  | 166                           | 0.2292                    | 85,187,130     | 60,504                   | 0.060504              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_300-16 | 7.450           | 108                    | 300                  | 200                           | 0.2440                    | 84,173,555     | 72,404                   | 0.072404              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_400-16 | 7.426           | 81                     | 400                  | 266                           | 0.2959                    | 82,411,590     | 96,204                   | 0.096204              |
| BenchmarkIBC_PrepareProposal_Update_Client_Multi/number_of_validators:_500-16 | 7.435           | 65                     | 500                  | 333                           | 0.3309                    | 81,605,510     | 120,004                  | 0.120004              |

#### ProcessProposal: `BenchmarkIBC_ProcessProposal_Update_Client_Multi`

Benchmarks of an `8mb` containing the maximum number of IBC `UpdateClient` with different number of signatures:

| Benchmark Name                                                                | Block Size (MB) | Number of Transactions | Number of Validators | Number of Verified Signatures | Process Proposal Time (s) | Total Gas Used | Transaction Size (Bytes) | Transaction Size (MB) |
|-------------------------------------------------------------------------------|-----------------|------------------------|----------------------|-------------------------------|---------------------------|----------------|--------------------------|-----------------------|
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_2-16   | 7.457           | 5,586                  | 2                    | 1                             | 1.0388                    | 390,490,985    | 1,466                    | 0.001466              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_10-16  | 7.464           | 2,367                  | 10                   | 6                             | 0.4714                    | 210,605,480    | 3,373                    | 0.003373              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_25-16  | 7.465           | 1,138                  | 25                   | 16                            | 0.2771                    | 141,904,565    | 6,945                    | 0.006945              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_50-16  | 7.462           | 610                    | 50                   | 33                            | 0.1598                    | 112,364,505    | 12,895                   | 0.012895              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_75-16  | 7.452           | 416                    | 75                   | 50                            | 0.1227                    | 101,405,415    | 18,850                   | 0.018850              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_100-16 | 7.453           | 316                    | 100                  | 66                            | 0.1112                    | 95,833,915     | 24,800                   | 0.024800              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_125-16 | 7.462           | 255                    | 125                  | 83                            | 0.1012                    | 92,509,080     | 30,750                   | 0.030750              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_150-16 | 7.441           | 213                    | 150                  | 100                           | 0.1035                    | 89,947,710     | 36,700                   | 0.036700              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_175-16 | 7.432           | 183                    | 175                  | 116                           | 0.0878                    | 88,172,820     | 42,652                   | 0.042652              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_200-16 | 7.467           | 162                    | 200                  | 133                           | 0.0974                    | 87,369,345     | 48,401                   | 0.048401              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_225-16 | 7.451           | 144                    | 225                  | 150                           | 0.0789                    | 86,194,935     | 54,326                   | 0.054326              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_250-16 | 7.428           | 129                    | 250                  | 166                           | 0.0775                    | 85,109,730     | 60,444                   | 0.060444              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_300-16 | 7.450           | 108                    | 300                  | 200                           | 0.0879                    | 84,173,555     | 72,404                   | 0.072404              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_400-16 | 7.426           | 81                     | 400                  | 266                           | 0.0616                    | 82,411,590     | 96,204                   | 0.096204              |
| BenchmarkIBC_ProcessProposal_Update_Client_Multi/number_of_validators:_500-16 | 7.435           | 65                     | 500                  | 333                           | 0.0596                    | 81,605,510     | 120,004                  | 0.120004              |
<!-- markdownlint-enable -->