# Increase square size

Currently the max square size on Arabica, Mocha, and Mainnet is 128. A square size of 128 has 8 MiB of capacity for blobs (see [data square table](https://gist.github.com/rootulp/bbf10f6e9cf114816aaa994eb64b63a4)).

The max square size is the minimum of `GovMaxSquareSize` and `SquareSizeUpperBound`. If you need to produce squares larger than the max square size, you must override either or both of these values: `GovMaxSquareSize` and `SquareSizeUpperBound`.

## GovMaxSquareSize

`GovMaxSquareSize` is the maximum square size that can be set by governance. By default it is set to 64 (what Mainnet started with). You can increase the `GovMaxSquareSize` on a network by submitting a governance proposal (see [gist](https://gist.github.com/rootulp/fcf5160a6506dc23a228a90d68e356bd)).

## SquareSizeUpperBound

Currently the `SquareSizeUpperBound` is set to 128. If you need to increase the `SquareSizeUpperBound` you must modify the hard-coded constant in [versioned_consts.go](https://github.com/celestiaorg/celestia-app/blob/36c2bf8558aa7710a2f3aba8c1c383c9a1b520be/pkg/appconsts/versioned_consts.go#L14) and create a new binary.
