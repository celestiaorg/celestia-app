# Addrbook

This directory contains a small tool to convert a peer address txt file into an address book JSON file.

## Example Usage

1. Modify `peers.txt` to contain the addresses you want to include in the address book.
1. Run the tool:

    ```shell
    go run main.go
    ```

This should generate a file called `addrbook.json` in the current directory. Next, manually verify the contents of `output.json` and if everything looks good, publish it.
