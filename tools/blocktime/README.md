# Blocktime

`blocktime` is a simple tool to analyze block production rates of a chain. It scrapes the latest headers through the RPC endpoint of a provided node and calculates the average, min, max and standard deviation of the intervals between the last `n` blocks (default: 100).

To start a consensus node and expose the RPC endpoint, see the [docs](https://docs.celestia.org/how-to-guides/consensus-node).

## Usage

To compile the binary, run either `go install` or `go build`. The binary can then be used as follows:

```bash
./blocktime <node_rpc> [query_range]
```

As an example

```bash
$ ./blocktime http://localhost:26657 1000

Chain: mocha-3
Block Time (from 55775 to 56775):
	Average: 12.79s
	Min: 1.00s
	Max: 25.36s
	Standard Deviation: 6.279s
```
