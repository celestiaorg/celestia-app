# ADR 021: Restricted and Configurable Block Size

## Status

Implemented in <https://github.com/celestiaorg/celestia-app/pull/1772>

## Changelog

- 2023/05/12: Initial draft
- 2023/05/30: Update status

## Context

Currently, block sizes are controlled by three values, the smallest of which
will determine what the protocol considers to be a valid block size.

| Parameter        | Size   | Unit   | Description  | Value Control |
|------------------|--------|--------|--------------|---------------|
| `MaxBlockSizeBytes` | 100 | MiB | Maximum total size of the protobuf encoded block, a hard coded constant acting as a cap for `MaxBytes`. | Hard coded |
| `MaxBytes`     |  21 | MiB | Determines the valid size of the entire protobuf encoded block. Is a governance modifiable parameter and is capped by `MaxBlockSizeBytes`. Used to regulate the amount of data gossiped in the consensus portion of the network (using the current block gossiping mechanism). | Modifiable |
| `MaxSquareSize` | ~7.5 | MiB | Determines the maximum size of the original data square. Used to regulate storage requirements for celestia-node, and the size of the data availability header. Default set to 128. | Modifiable (versioned constant) |

Using the currently set/default values, `MaxSquareSize` is limiting the block
size because it is the smallest value in terms of bytes. However if the
community changed `MaxBytes` to something below 7.5MiB, then that would become
the limit on blocksize.

The system is missing the ability for governance to limit the square size, which
is something that we want in order to reduce the amount of data stored by the
data availability portion of the network. This could be done now by starting new
networks with `MaxSquareSize` set to 64. However, this would mean that even
after a solution is implemented for pruning, a hardfork would be required to
increase the value back to the current `MaxSquareSize`.

### Reduce the `MaxBytes` Governance Parameter

One solution is to reduce the `MaxBytes` governance parameter to roughly the
size of a 64 x 64 square (~1.8 MB). While tendermint will reject blocks that
exceed `MaxBytes`, due to encoding overhead, it's possible for a significantly
smaller block to require a square size larger than 64. This results in only ever
confidently achieving a soft block of squares over size 64. Implementation and
fuzzing test for this option can be found at
[#1743](https://github.com/celestiaorg/celestia-app/pull/1743).

### Introduce a new Governance Parameter

The second suggestion is to create a new governance parameter,
`GovMaxSquareSize`, which governance could set. Now the protocol would use the
lowest of 4 values to determine the size of a block at a given height. This has
the benefit of allowing for the most flexibility in terms of options, however it
has the downside of exposing a rather technical parameter to governance. Full
implementation in [#1772](https://github.com/celestiaorg/celestia-app/pull/1772)

### Use `MaxBytes` Governance Parameter to Limit Square Size

The third suggested solution is to use the existing governance parameter to also
contribute to limiting the square size. This would allow for governance to set
the `MaxBytes` value, and then the application would use that value along with
the `MaxSquareSize` to determine the `GovMaxSquareSize` which would be used to
create the square. If governance wanted to limit the square size, then it would
set a value that is smaller than or equal to the optimal value of a given square
size. This has the benefit of not exposing another parameter to governance, but
it makes the result of the currently exposed parameter, `MaxBytes`, more
complicated. It also eliminates the possibility to allow for small blocks with a
lot of encoding overhead, since the only way to increase the square size is to
also increase the `MaxBytes`.Full implementation in
[#1765](https://github.com/celestiaorg/celestia-app/pull/1765)

Note that there is technically a fourth solution, where the `GovMaxSquareSize`
is added, and it determines the value for `MaxBytes`. The result of this
solution is effectively identical to just adding a new `GovMaxSquareSize`
parameter.

## Decision

Option 2: Introduce a new parameter, `GovMaxSquareSize`. After implemented, the above chart will look like:

| Parameter        | Size   | Unit   | Description  | Value Control |
|------------------|--------|--------|--------------|---------------|
| `MaxBlockSizeBytes` | 100 | MiB | Maximum total size of the protobuf encoded block, a hard coded constant acting as a cap for `MaxBytes`. | Hard coded |
| `MaxBytes`     |  ~1.8 | MiB | Determines the valid size of the entire protobuf encoded block. Is a governance modifiable parameter and is capped by `MaxBlockSizeBytes`. Used to regulate the amount of data gossiped in the consensus portion of the network (using the current block gossiping mechanism). | Modifiable |
| `MaxSquareSize` | ~7.5 | MiB | Determines the maximum size of the original data square. Used to regulate storage requirements for celestia-node, and the size of the data availability header. Default set to 128. | Modifiable (versioned constant) |
| `GovMaxSquareSize`     |  ~1.8 | MiB | Governance modifiable parameter that determines valid square sizes. Must be smaller than the `MaxSquareSize`. Default set to 64. | Modifiable |

## Detailed Design

code copied from the full implementation in
[#1772](https://github.com/celestiaorg/celestia-app/pull/1772)

we first introduce a new parameter

```proto
// Params defines the parameters for the module.
message Params {
  ...

  uint64 gov_max_square_size = 2
      [ (gogoproto.moretags) = "yaml:\"gov_max_square_size\"" ];
}
```

```go
// GovMaxSquareSize returns the maximum square size that can be used for a block
// using the governance parameter blob.GovMaxSquareSize.
func (app *App) GovMaxSquareSize(ctx sdk.Context) int {
	// TODO: fix hack that forces the max square size for the first height to
	// 64. This is due to tendermint not technically supposed to be calling
	// PrepareProposal when heights are not >= 1. This is remedied in versions
	// of the sdk and comet that have full support of PrepareProposal, although
	// celestia-app does not currently use those. see this PR for more
	// details https://github.com/cosmos/cosmos-sdk/pull/14505
	if ctx.BlockHeader().Height == 0 {
		return int(appconsts.DefaultGovMaxSquareSize)
	}

	gmax := app.BlobKeeper.GovMaxSquareSize(ctx)
	// perform a secondary check on the max square size.
	if gmax > appconsts.MaxSquareSize {
		gmax = appconsts.MaxSquareSize
	}

	return int(gmax)
}

func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	...

	// build the square from the set of valid and prioritised transactions.
	// The txs returned are the ones used in the square and block
	dataSquare, txs, err := square.Build(txs, app.GovMaxSquareSize(sdkCtx))
	if err != nil {
		panic(err)
	}
    ...
}

func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
    ...

	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(req.BlockData.Txs, app.GovMaxSquareSize(sdkCtx))
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to compute data square from transactions:", err)
		return reject()
	}
    ...
}
```

## References

- Option 1 was implemented in [#1743](https://github.com/celestiaorg/celestia-app/pull/1743)
- Option 2 was implemented in [#1772](https://github.com/celestiaorg/celestia-app/pull/1772)
- Option 3 was implemented in [#1765](https://github.com/celestiaorg/celestia-app/pull/1765)
- Issue to restrict the block size in a configurable way [#1592](https://github.com/celestiaorg/celestia-app/issues/1592)
- Decision to limit the block size [#1737](https://github.com/celestiaorg/celestia-app/issues/1737)
- Original issues to add `MaxBlockSize` parameters [#183](https://github.com/celestiaorg/celestia-app/issues/183)
