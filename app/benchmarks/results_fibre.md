# Fibre (PayForFibre) gas benchmarks

## Why these exist

When a node verifies a PFF's validator signatures, the gas meter is off (it
has to be — some nodes skip verification via the cache, and gas must be the
same for everyone). So instead of metering the work, the chain charges a flat
fee, from `pkg/appconsts/fibre_gas_consts.go`:

```
gas = 75_000 + 1_000 per validator signature
```

Those two numbers were guesses (`TODO: benchmark and tune`). The benchmarks
answer one question: **do the guesses match the real work?**

The problem is shaped like a taxi fare: total = base fee + per-km rate. We can
only ever measure *totals* (verify with n signatures, get one time), so we
take totals at several n and work out the base fee and the rate.

## Step 1 — time the work at different sizes

| signatures (n) | measured time |
| ---: | ---: |
| 1 | 109 µs |
| 10 | 252 µs |
| 30 | 652 µs |
| 100 | 1,952 µs |

## Step 2 — split into "base fee" and "per signature"

Draw the best straight line through those four points
(`TestPFFGasConstantsVsMeasuredCost` does this):

```
time(n) ≈ 69 µs  +  20.7 µs × n
          base      per signature
```

Every PFF costs ~69 µs no matter what; each validator signature adds ~20.7 µs.
The line is straight — the cost of one signature does not depend on how many
there are.

## Step 3 — what should that cost in gas?

Gas is a receipt for CPU time. The chain's going rate, measured from a
MsgSend: **1 gas buys ~2.5 ns of work**. At that rate the fair prices are:

- base: 69,000 ns → **~28,000 gas** (charged: 75,000 — overpaying, fine)
- one signature: 20,700 ns → **~8,400 gas** (charged: 1,000 — an **8x
  discount**)

(A second defensible rate exists: the SDK prices raw crypto ~30x above its
CPU cost — one secp256k1 verify is decreed to be 1,000 gas but takes 77 µs.
Measured against *that* rate the constants look generous. But Celestia runs
with no block gas limit (`MaxGas = -1`), so gas here is purely what users
*pay for* — fee = gas × gas price — and the going rate above is what makes
fees proportional to the CPU actually consumed.)

## Step 4 — why the discount is invisible in small cases

The wrong unit price exists even for a single tx with a single signature —
that signature does 20.7 µs of work and pays 1,000 gas. But the total bill
still looks fine there, because the overpriced base (+47,000 gas surplus)
hides the one underpriced signature (−7,400 deficit). A 1-validator PFF in
fact lands at exact work-per-gas parity with a MsgSend.

The base surplus is one-time; the signature deficit repeats per signature:

| signatures | base surplus | signature deficit | net |
| ---: | ---: | ---: | --- |
| 1 | +47,000 | −7,400 | overpays |
| ~6 | +47,000 | −44,000 | break-even |
| 100 | +47,000 | −740,000 | pays ~1/4 of its work |

So small valsets mask the error; big ones multiply it. The per-signature
constant — the term that scales — is the one that's mispriced.

## Worst case: a full block of PFFs

`BenchmarkFinalizeBlock_PFFWorstCase` runs the heaviest legal block through
FinalizeBlock: 200 PFFs (the production cap), each with signatures from a
100-validator set, none cached — every tx pays ante with real signature
verification, stateless + stateful promise checks, and escrow settlement.

Result (Apple M5 Pro): **~417 ms per block, ~2.1 ms per PFF.** Expect 2-3x
more on recommended validator hardware, and the same work repeats in
prepare/process on the proposer and every validator.

Two things follow. First, with no block gas limit, the only thing bounding
this is the 200-PFF count cap — that cap is load-bearing for block time, so
treat changes to it as performance changes. Second, the discount means this
CPU is cheap to buy: the block above does ~3.7x more processing per unit of
gas (= per utia of fees) than a MsgSend block — an attacker rents validator
CPU at a fraction of the price everyone else pays.

**Fix:** raise `PFFibreGasPerValidatorSignature` toward ~8,000, or make
verification cheap on replay (cache verification results per promise) so the
low price becomes honest.

## Running them

One benchmark per process — sharing a process skews later results 2-3x:

```shell
go test -tags=benchmarks -run='^$' -bench='BenchmarkPFFSignatureVerification$' -benchtime=2s ./app/benchmarks/
go test -tags=benchmarks -run='^$' -bench='BenchmarkFinalizeBlock_PFFWorstCase$' -benchtime=2s ./app/benchmarks/
go test -tags=benchmarks -run=TestPFFGasConstantsVsMeasuredCost -v ./app/benchmarks/
```

## Fine print

- Times are from a fast dev machine, but the *ratios* (work time vs. anchor
  time) cancel out machine speed, so the gas conclusions carry over.
- Benchmark validators have equal stake; real stake distributions shift how
  many signatures get verified before the 2/3-stake early exit.
- The other fibre constants (`PFBFibreGasFixedCost`, `PFBFibreGasPerChunk`)
  price data-availability settlement, not CPU — out of scope here.
