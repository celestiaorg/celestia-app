# Chainbuilder

`chainbuilder` is a tool for building a Celestia chain for testing and development purposes.

## Usage

Use `go` to run the binary as follows:

```
go run ./tools/chainbuilder
```

This will create a directory with the name `testnode-{chainID}`. All files will be populated and blocks generated based on specified input. You can run a validator on the file system afterwards by calling:

```
celestia-appd start --home /path/to/testnode-{chainID}
```

The following are the set of options when generating a chain:

- `num-blocks` the number of blocks to be generated (default: 100)
- `block-size` the size of the blocks to be generated (default <2MB). This will be a single PFB transaction
- `square-size` the size of the max square (default: 128)
- `existing-dir` point this to a directory if you want to extend an existing chain rather than create a new one
- `namespace` allows you to pick a custom v0 namespace. By default "test" will be chosen.

This tool takes roughly 60-70ms per 2MB block.


