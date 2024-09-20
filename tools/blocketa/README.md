# Block ETA (Estimated Time of Arrival)

blocketa is a tool that estimates the time of arrival of a block height.

## Usage

```shell
$ go run main.go https://celestia-mocha-rpc.publicnode.com:443 2585031
chainID: mocha-4
currentHeight: 2580660
currentTime: 2024-08-28 02:46:32.933542677 +0000 UTC
diffInBlockHeight: 4371
diffInTime: 14h37m50.55s
arrivalTime: 2024-08-28 17:24:23.483542677 +0000 UTC
```

> [!NOTE]
> The block time is currently hard-coded. If you're running this for a network with a different block time, you'll need to update the `blockTime` constant in the main.go file. You can use [https://www.mintscan.io/celestia/block](https://www.mintscan.io/celestia/block/) or the blocktime tool.
