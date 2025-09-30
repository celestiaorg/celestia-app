# check-eds-construction

`check-eds-construction` verifies that the current EDS construction method produces the same data root as the one found in a produced block. It fetches a block via a node's RPC, reconstructs the EDS from the block's transactions, computes the DAH, and compares hashes.

## Usage

Build or run with Go:

```bash
go run ./tools/check-eds-construction <node_rpc> <block_height>
# or
go build -o check-eds-construction ./tools/check-eds-construction
./check-eds-construction <node_rpc> <block_height>
```

- `<node_rpc>`: CometBFT HTTP RPC endpoint (e.g., `http://localhost:26657` or `https://rpc.celestia-arabica-11.com:443`).
- `<block_height>`: Height of the block to verify.

## Example

```bash
$ go run ./tools/check-eds-construction https://rpc.celestia-arabica-11.com:443 123456
Connected to https://rpc.celestia-arabica-11.com:443 on chain arabica-11
Got data root: 3C7F...ABCD
Computed data root: 3C7F...ABCD
```

If the two hashes match, the construction method is compatible with currently produced blocks. A mismatch indicates an incompatibility or configuration issue.

## Notes

<<<<<<< HEAD
- Requires access to a live node's RPC endpoint.
- The tool uses the block's App version when reconstructing the EDS.
- Ensure the target height exists and the node is synced.


=======
- Requires access to a live node's RPC endpoint
- The tool uses the block's App version when reconstructing the EDS
- For `random` mode, blocks are selected randomly from height 2 to the latest block height
- Ensure the target height exists and the node is synced
>>>>>>> 9c1e04d (feat: add open telemetry traces (#5707))
