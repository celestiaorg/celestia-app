# ADR 021: Restricted and Configurable Block Size

## Changelog

- 2023-05-12: Initial

## Status

Accepted

## Context

Currently, block sizes are controlled by three values, the smallest of which will determine what the protocol considers to be a valid block size.

- `MaxBlockBytes` is a hard coded constant to the maximum total size of the protobuf encoded block, and therefore acts as a cap for `MaxBytes`. Currently set to 100 MB.
- `MaxBytes` is similar to `MaxBlockBytes` in that it determines the valid size of the entire protobuf encoded block, but it is a governance modifiable parameter and is capped by `MaxBlockBytes`. Having control over this value is useful to regulate the amount of data gossiped in the consensus portion of the network (using the current block gossiping mechanism). Currently, the default is 21 MB.
- `MaxSquareSize` determines the maximum size of the original data square. Currently set to 128, which equates to ~7.5 MB of usable blockspace. At the moment, having control of this value allows us to regulate storage requirements for celestia-node, and the size of the data availability header.

Using the currently set/default values, `MaxSquareSize` is limiting the block size because it is the smallest value in terms of bytes.

The system is missing the ability for governance to limit the square size, which is something that we want in order to reduce the amount of data stored by the data availability portion of the network. This could be done now by starting new networks with `MaxSquareSize` set to 64. However, this would mean that even after a solution is implemented for pruning, a hardfork would be required to increase the value back to the current `MaxSquareSize`.

### Reduce the `MaxBytes` Governance Parameter

One solution is to reduce the `MaxBytes` governance parameter to roughly the size of a 64 x 64 square (~1.8 MB). While tendermint will reject blocks that exceed `MaxBytes`, due to encoding overhead, it's possible for a significantly smaller block to require a square size larger than 64. This results in only ever confidently achieving a soft block of squares over size 64. Implementation and fuzzing test for this option can be found at [#1743](https://github.com/celestiaorg/celestia-app/pull/1743).

### Introduce a new Governance Parameter

The second suggestion is to create a new governance parameter, `GovMaxSquareSize`, which governance could set. Now the protocol would use the lowest of 4 values to determine the size of a block at a given height. This has the benefit of allowing for the most flexibility in terms of options, however it has the downside of exposing a rather technical parameter to governance.

### Use `MaxBytes` Governance Parameter to Limit Square Size

The third suggested solution is to use the existing governance parameter to also contribute to limiting the square size. This would allow for governance to set the `MaxBytes` value, and then the application would use that value along with the `MaxSquareSize` to determine the `GovMaxSquareSize` which would be used to create the square. If governance wanted to limit the square size, then it would set a value that is smaller than or equal to the optimal value of a given square size. This has the benefit of not exposing another parameter to governance, but it makes the result of the currently exposed parameter, `MaxBytes`, more complicated. It also eliminates the possibility to allow for small blocks with a lot of encoding overhead, since the only way to increase the square size is to also increase the `MaxBytes`. 

Note that there is technically a fourth solution, where the `GovMaxSquareSize` is added, and it determines the value for `MaxBytes`. The result of this solution is effectively identical to just adding a new `GovMaxSquareSize` parameter.

## Decision

Option 2: Introduce a new parameter, `GovMaxSquareSize`.

## Detailed Design

TBD

## References

- Option 1 was implemented in [#1743](https://github.com/celestiaorg/celestia-app/pull/1743)
- Issue to restrict the block size in a configurable way [#1592](https://github.com/celestiaorg/celestia-app/issues/1592)
- Decision to limit the block size [#1737](https://github.com/celestiaorg/celestia-app/issues/1737)
- Original issues to add `MaxBlockSize` parameters [#183](https://github.com/celestiaorg/celestia-app/issues/183)