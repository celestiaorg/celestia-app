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

## How to use the script

```shell
./scripts/signing/verify-signature.sh checksums.txt.sig checksums.txt
```

should see output like this:

```shell
Importing the public keys in ./keys...
Verifying the signature of /Users/rootulp/Downloads/checksums.txt.sig with /Users/rootulp/Downloads/checksums.txt...
gpg: Signature made Thu Sep 21 14:39:26 2023 EDT
gpg:                using EDDSA key ACF99399A35311E95B2432072B987E2A363550BE
gpg: Good signature from "rootulp-test-goreleaser <rootulp@gmail.com>" [ultimate]
```
