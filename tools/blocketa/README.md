# Block ETA (Estimated Time of Arrival)

blocketa is a tool that estimates the time of arrival of a block height.

## Usage

```shell
$ go run main.go https://rpc-mocha.pops.one:443 1150000
chainID: mocha-5
currentHeight: 1134827
currentTime: 2026-09-26 13:56:26.546771175 +0000 UTC
diffInBlockHeight: 15173
diffInTime: 49h31m23s
arrivalTime: 2026-09-28 15:27:49.546771175 +0000 UTC
```

> [!NOTE]
> The block time is currently hard-coded. If you're running this for a network with a different block time, you'll need to update the `blockTime` constant in the main.go file. You can use [https://www.mintscan.io/celestia/block](https://www.mintscan.io/celestia/block/) or the blocktime tool.
