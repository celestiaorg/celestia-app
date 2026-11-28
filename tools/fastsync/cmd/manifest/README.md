# manifest

Builds, signs, and verifies the manifest of a changeset bundle: a directory of `cs-<height>[-<seq>].bin[.zst]`
files written by the changeset recorder.

```bash
cd tools/fastsync && make build   # ./build/manifest

# once, offline: the private key never leaves the release machine; publish bundle.pub in the repo
manifest keygen -key bundle.key -pub bundle.pub   # or: openssl genpkey -algorithm ed25519 -out bundle.key

# publisher
manifest build -dir BUNDLE -chain-id celestia -last-height H -expected expected.tsv -out manifest.json
manifest sign  -manifest manifest.json -key bundle.key            # writes manifest.json.sig

# user, before applying
manifest verify -dir BUNDLE -manifest manifest.json -sig manifest.json.sig -pub bundle.pub -chain-id celestia
```

## Manifest

The manifest is JSON with fields in a fixed order. It holds the format version, the chain id, the first and
last height, `last_app_hash` (the app hash after `last_height`, i.e. the AppHash of header `last_height+1`,
the same convention as `fshdr`'s `expected.tsv`), the total size, and one entry per file: name, size, sha256
and, for changeset files, the file's height range.

- A file's first height comes from its name. Its last height is inferred from the next file's first height,
  and the last file ends at `-last-height`. If the recorder restarted and re-recorded heights (several files
  start at the same height, e.g. `cs-H.bin.zst` and `cs-H-1.bin.zst`), pass `-ranges` with each file's real
  range. `build` refuses gaps.
- Other files (e.g. `expected.tsv.zst`) are included with `-extra NAME`. Any other file in the directory fails
  `build`, including a leftover `*.zst.tmp`, both `.bin` and `.bin.zst` of one file, subdirectories and
  symlinks. What gets published is exactly what was hashed.

## Signature

The signature is a detached ed25519 signature over the exact bytes of `manifest.json`. The signature file
holds it as base64. Keys are PEM: PKCS#8 for the private key, PKIX for the public key, the same format
`openssl` writes. Anyone can check a signature without this tool:

```bash
base64 -d manifest.json.sig > sig.bin
openssl pkeyutl -verify -pubin -inkey bundle.pub -rawin -in manifest.json -sigfile sig.bin
```

## What verify checks, in order

1. The signature, before the manifest is even parsed.
2. The manifest itself: strict JSON (unknown fields and trailing data are rejected), a known version, valid
   hashes and sizes, and file names that are plain base names (`[A-Za-z0-9][A-Za-z0-9._-]*`, no `..`). That
   rules out path traversal even under a validly signed manifest.
3. The file set: no file outside the manifest (use `-allow-extra` or `-ignore NAME` to override), none
   missing, every entry a regular file (no symlinks or directories).
4. All sizes, before any hashing, so a truncated download fails fast.
5. Every sha256, streamed with a 1 MiB buffer per worker (`-j`, default 4). Memory use does not depend on
   file size.

`-print-env` prints `CHAIN_ID`, `FIRST_HEIGHT`, `LAST_HEIGHT` and `LAST_APP_HASH` lines for scripts
(`scripts/orchestrate/apply-checkpoints.sh` uses it). Exit status 0 means verified. Anything else means do not use
the bundle.

The format and checks live in `internal/manifest`; this command adds keys, signing and the CLI.

The manifest proves only that the publisher's key vouched for these bytes. The appliers still check every
height against the hash-chain-verified headers, so a correctly signed bad bundle cannot produce a wrong state.
