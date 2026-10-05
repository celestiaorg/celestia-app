# Corto-10 build and ship recipe

How the deployed Corto-10 arm64 binaries were built and distributed. Hosts are
`c8gn.12xlarge` (arm64, Ubuntu 24.04) in eu-central-1.

## Build (linux/arm64)

Both binaries are built static (`CGO_ENABLED=0`) for `linux/arm64`.

```sh
# celestia-appd (standalone, build tag: ledger)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 make build-standalone   # -> build/celestia-appd

# fibre server
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off make build-fibre-server   # -> build/fibre
```

`GOWORK=off` forces the build to honour `go.mod`, whose
`replace github.com/klauspost/reedsolomon => ./third_party/reedsolomon` selects
the vendored Reed-Solomon fork (NEON / EOR3 arm64 kernels). See
`third_party/reedsolomon/FORK.md`. Go `replace` directives are not inherited by
downstream modules, so fibre-gateway vendors its own copy of the same fork.

`build.sh` runs both builds and prints each binary's sha256.

## Ship (content-addressed S3, sha-verified install)

Artifacts are stored content-addressed; the sha256 is part of the key. Never scp.

```sh
SHA=$(sha256sum build/celestia-appd | cut -d' ' -f1)
aws s3 cp build/celestia-appd \
  s3://corto-10-867703356141-20260929/artifacts/$SHA/celestia-appd
# likewise for build/fibre
```

Install on each host fetches the object by presigned URL, verifies the sha256
matches the key before use, atomically replaces the running binary and keeps a
`.prev-<sha>` backup for rollback. That install path is `scripts/rollout.py` in
`celestiaorg/infrastructure` (`ansible/corto/corto-10/`).

## Deployed appd provenance

The deployed `celestia-appd` reports version `10.1.0-corto-145-gab7bad936`.

That string is exactly `git describe --tags --long ab7bad936` with the leading
`v` stripped, which is what the Makefile `VERSION` stamp does:

```
$ git describe --tags --long ab7bad936
v10.1.0-corto-145-gab7bad936
```

So the deployed binary was built with `make build-standalone` at commit
`ab7bad936` on this branch (`perf/encoder-arm-opt-sdk-20260929`) — 145 commits
past tag `v10.1.0-corto`.

The audit's "built from candidate `f2d5874ba` + `corto-10-app-candidate.patch`"
describes a **different** artifact: a darwin/arm64 multiplexer binary built only
to capture the Corto-10 config/genesis baselines
(`corto-10-config-baselines-20260929/candidate-source-manifest.json`). It is not
the deployed appd. `f2d5874ba` is an ancestor of `ab7bad936` (56 commits back),
and the patch's changes (app v11, fibre ante, valaddr) are already real commits
in the branch history at `ab7bad936`, so a from-source build there needs no
patch and reproduces the same app-v11 parameter set.

| artifact | source | build |
|---|---|---|
| deployed `celestia-appd` (linux/arm64) | `perf/encoder-arm-opt-sdk-20260929` @ `ab7bad936` | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 make build-standalone` |
| deployed `fibre` (linux/arm64) | same commit `ab7bad936` | `… GOWORK=off make build-fibre-server` |
| config-baseline binary (darwin/arm64, not deployed) | `f2d5874ba` + `corto-10-app-candidate.patch` | `make build` (multiplexer) |

The handoff stamps `celestia-appd 22eb7b17` and `fibre f54d4470` are off-lineage
and do not correspond to the deployed source; the version string above is
authoritative.
