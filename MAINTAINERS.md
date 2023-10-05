# Maintainers

This page includes information for maintainers of this repo.

## How to manually generate the binaries for a Github release

The binaries for the Github release are generated using [GoReleaser](https://goreleaser.com/). The Github workflow [ci-release.yml](./.github/workflows/ci-release.yml) should automatically create pre-built binaries and attach them to the release.

> **NOTE** Due to `goreleaser`'s CGO limitations, cross-compiling the binary does not work. So the binaries must be built on the target platform. This means that the release process must be done on a Linux amd64 machine.

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
    ├── config.yaml
    └── metadata.json
```

For the Github release, you just need to upload the `checksums.txt` and `celestia-app_Linux_x86_64.tar.gz` files.

> **NOTE** Since you built the binaries manually, the checksums.txt won't automatically be signed with the GPG key associated with celestia-app-maintainers so there won't be a `checksums.txt.sig` to upload.
