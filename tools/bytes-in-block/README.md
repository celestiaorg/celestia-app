# bytes-in-block

bytes-in-block is a tool that calculates the average block size and block time for a range of blocks. To use it, modify the constants in main.go and run

```shell
go run main.go
```

Example output

```
$ go run main.go
Fetched a total of 110 blocks (from ~4170798 up to ~4170898).
Average bytes_in_block: 5617659.89 bytes (~5.36 MiB)
Average block_time:     6761.83 ms (~6.76 seconds)
```
