# Maintainers

This page includes information for maintainers of this repo.

## How to manually generate the binaries for a Github release

The binaries for the Github release are generated using [GoReleaser](https://goreleaser.com/). The Github workflow [ci-release.yml](./.github/workflows/ci-release.yml) should automatically create pre-built binaries and attach them to the release.

### Prerequisites

1. Due to `goreleaser`'s CGO limitations, cross-compiling the binary does not work. So the binaries must be built on the target platform. This means that the release process must be done on a Linux amd64 machine.

1. Since you are generating and signing the release binaries locally, your public key must be added to the list of available keys for verification. Follow the steps in [scripts/verify-signature/README.md](./scripts/verify-signature/README.md).

### Steps

Export environment variables for the GPG key you are using. You can get this value by running `gpg --list-keys`.

```shell
export GPG_FINGERPRINT=6C1A1C23002059AF36D176ADD81D0045A524FA93
```

To generate the binaries for the Github release, you can run the following command:

```sh
make goreleaser-release
```

This will generate the binaries as defined in `.goreleaser.yaml` and put them in `build/goreleaser` like so:

```sh
build
└── goreleaser
    ├── CHANGELOG.md
    ├── artifacts.json
    ├── celestia-app_Linux_x86_64.tar.gz
    ├── celestia-app_linux_amd64_v1
    │   └── celestia-appd
    ├── checksums.txt
    ├── checksums.txt.sig
    ├── config.yaml
    └── metadata.json
```

For the Github release, you need to upload the following files:

- `checksums.txt`
- `checksums.txt.sig`
- `celestia-app_Linux_x86_64.tar.gz`
