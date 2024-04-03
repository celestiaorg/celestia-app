# ntrace

A tool for visualizing message passing in a distributed system based on local timestamps of the nodes that are sending and receiving messages.

## Usage

```bash
go run tools/ntrace/main.go
```

It should create a SVG file that looks like this:

![ntrace example](./example.svg)
