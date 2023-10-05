# Signing

This directory contains a script for consumers to verify signatures on artifacts. The `keys` directory contains GPG public keys for some of the celestia-app maintianers. The keys may be used to sign releases and other artifacts.

## How to add a public key

### Prerequisite

1. [Generate a GPG key](https://docs.github.com/en/authentication/managing-commit-signature-verification/generating-a-new-gpg-key) with no passphrase

### Steps

1. Export your public key

    ```shell
    gpg --armor --export <your-key-id> <your-key-id>.asc
    ```

1. Copy the `*.asc` file into `keys/`
