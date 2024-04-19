# Blockscan

This is an inspection tool to scan blocks and display the contents of the transactions that fill them. 

## Usage

The format for running blockscan is as follows:

```bash
Usage: blockscan <rpc-address> [from-height] [to-height]
```

Here is an example commang for following the head of mainnet:

```bash
go run ./tools/blockscan https://rpc.lunaroasis.net:443
```

There are three options for scanning transactions:

- **Trail**: This sets up a websocket and follows the head of the chain until the command is interrupted or killed
- **Range**: This returns information of all transactions across an inclusive range: `go run ./tools/blockscan https://rpc.lunaroasis.net:443 100 200`
- **Single**: This returns the information of a single block: `go run ./tools/blockscan https://rpc.lunaroasis.net:443 100`
