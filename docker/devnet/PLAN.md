# Publish reusable devnet images from celestia-app

## High Level Goal

Facilitate easier testing of celestia adjacent projects.
To have something like ganache for ethereum, so everyone can run 2 containers or copy/paste simple docker compose.
It should be reliable and fast to start.

## Summary

Centralize the publishable devnet implementation in celestia-app. 
Each app release tag should produce a tested validator image and bridge image 
that can start a useful local network with sane defaults, 
runtime configuration, Fibre support, and persistent state.

The first version publishes a one-validator topology with one Fibre server and one bridge. 
It supports multiple funded client accounts. Configurable consensus-validator counts, downstream repository migrations, 
and authenticated bridge RPC are follow-up work. The existing two-validator `local_devnet` example remains available.

## Current release and devnet implementations

A maintainer creates a release branch, chooses a semantic version tag in GitHub, and publishes a release or prerelease.
Release publication triggers binary generation. A separate Docker workflow responds to `v*` tag pushes.
Devnet publication should follow the tag-driven Docker process and include release candidates and network-specific prereleases.

// Nikolai: insert a permalink
Sovereign SDK currently builds and pushes its validator and bridge images manually. 
It provides a useful single-validator setup with multiple funded bridge accounts,
image health checks, shared credentials, and a genesis-hash handoff. 
Its account-count setting creates funded bridge accounts, not multiple consensus validators.

// Nikolai: insert a permalink
// Nikolai: Lumina has been updated, review
Lumina builds local validator, node, and proxy images for CI. 
It provides useful runtime configuration, supplied-key support, 
bridge and light-node startup, optional RPC authentication, and specialized integration-test topologies. 
Those specialized topologies and grpcwebproxy remain consumer-specific.

The existing celestia-app `local_devnet` provides two validators, two Fibre servers, persistent initialization, 
Fibre provider registration, escrow funding, and PFB/PFF checks. 
Its Fibre setup and verification tools should be reused without replacing that example.

## Images and runtime behavior

Add the canonical publishable implementation under `docker/devnet/`.

Publish these images under the same celestia-app release tag:

- `ghcr.io/celestiaorg/celestia-devnet-validator:<app-tag>` contains `celestia-appd`, `fibre`, provisioning scripts, and the test helper, all built from the tagged celestia-app checkout.
- `ghcr.io/celestiaorg/celestia-devnet-bridge:<app-tag>` contains a repository-pinned celestia-node release and the bridge startup script.

Both images support `linux/amd64` and `linux/arm64`. 
The bridge base image is `ghcr.io/celestiaorg/celestia-node`, pinned by tag and digest.
Docker Hub is not used: anonymous pulls from shared runner IPs hit rate limits, which the org pipeline already works around with retries. 
// Nikolai: What is this and why?
The `v0.34.2-mocha` index digest is `sha256:d66a22770edaeb0bcadafa3e40b88ff86422d38f51eb07dd3e34566520ecdf2b` and lists both Linux architectures. 
Weekly Dependabot PRs update tag and digest, and the devnet compatibility suite must pass before such an update merges. An app release therefore uses the celestia-node version already tested and recorded in its source tag.


Provide a published-image Compose example and a source-build override. 
The default stack runs one validator, one Fibre server, one bridge, and a one-shot provisioning service. 
// Nikolai: No, can fibre run as part of validator? Is it part of the same binary or do we need 2 binaries to have fibre
Fibre runs as a separate container from the validator image.


On first boot, 
// Nikolai: can this be done during container building?
the validator generates keys, funds accounts in genesis, and exports client credentials. 

Callers may supply deterministic account keys before initialization. 
Validator, bridge, Fibre, credentials, and stored blobs persist across container restarts. 
Deleting Compose volumes is the explicit reset operation.

// Nikolai: just to confirm, those are filesystem in container? 
The credential files are the image's public API and stay stable: 
`/credentials/node-N.key` (armored, password `password`), `node-N.plaintext-key` (hex), `node-N.addr`, and `validator-0.key`, `validator-0.plaintext-key`, `validator-0.addr`, `validator-0.valaddr`. 
These are the names lumina's tests compile in. The validator keyring name stays `validator`.

Startup order is validator, Fibre, provisioning, then bridge. 
Provisioning registers the Fibre address, funds Fibre escrow for every client account, waits for each transaction to commit, and fails if it cannot complete. 
Validator and bridge image health checks verify usable protocol state rather than an open port. All startup waits are bounded.

Bridge RPC authentication is disabled in version 1. 
Published host ports bind to loopback. The validator signing port remains internal. 
Containers run as root. Version 1 supports neither light nodes nor authenticated bridge RPC, so lumina's auth-enabled and light-node services cannot migrate yet.

Expose these settings:

| Setting                                  | Default                            |
|------------------------------------------|------------------------------------|
| Network and chain ID                     | `devnet`                           |
| Funded account count                     | `1`, with support for at least 10  |
| Bridge account                           | First funded account               |
| Account funding                          | `1000000000000000utia` per account |
| Fibre escrow                             | `1000000000000utia` per account    |
| Block timing                             | `1s` delayed precommit timeout     |
| Fibre advertised address                 | `localhost:7980`                   |
| Block size, square size, and mempool TTL | Binary defaults unless overridden  |

Expose validator connection settings for the bridge. 
Document the Fibre advertised-address override for clients that run in another Docker network. 
Run Fibre and bundled tools in the validator network namespace so the default `localhost` address works for the included stack.

## CI and publication

Add one devnet workflow with three stages.

1. Build and test on pull requests that touch `docker/devnet/**`, `.github/workflows/devnet.yml`, or `local_devnet/submit/**`, on all merge-queue entries (GitHub has no path filters for that event), on `v*` tag pushes, and on manual dispatch. This matches how the E2E image build is gated to the merge queue and release branches. Use native amd64 and arm64 runners. Build both images, start the complete stack, and run the smoke suite.
2. On upstream tag pushes or manual dispatch targeting an existing tag, upload the exact tested per-architecture images to GHCR. Do not rebuild after testing.
3. After both architectures publish successfully, assemble the validator and bridge multi-architecture manifests under the exact app tag and verify that each contains amd64 and arm64 Linux images.

This is a dedicated workflow rather than more jobs in `docker-build-publish.yml` on the org's reusable pipeline: native arm64 runners replace QEMU emulation, and the exact per-architecture images that passed the smoke suite are the ones published, with the manifest assembled only after both pass. Consequences, decided explicitly: GHCR only, no docker.io mirror, no sha or `latest` tags.

Keep registry credentials and package-write permissions confined to publication jobs. Preserve logs on failure. Do not publish `latest` or `main` aliases. Per-architecture tags `<tag>-amd64` and `<tag>-arm64` remain published next to the manifest tag. Leave existing production image publication unchanged.

Update release documentation with the image names, usage, architecture verification, and manual-rerun procedure. The GHCR packages must allow anonymous pulls.

## Acceptance criteria

Run these checks on both architectures:

- A fresh startup produces blocks, completes Fibre provisioning, and synchronizes the bridge.
- A classic blob can be submitted and retrieved through the bridge.
- A Fibre blob can be uploaded and downloaded with byte-for-byte verification.
- After stopping and restarting without deleting volumes, account addresses remain unchanged, block production continues, and previously stored classic and Fibre blobs remain retrievable.
- Supplied keys, multiple funded accounts, a nondefault network, advertised address, block timing, block size, square size, and mempool TTL work.
- Missing dependencies and failed provisioning produce bounded failures.
- Shell scripts, Dockerfiles, Compose files, workflow syntax, and documentation pass their repository checks. `make lint` runs hadolint on `docker/devnet/Dockerfile`; hadolint is wired in `lint.yml` and the Makefile, not in the devnet workflow.
- `local_devnet/e2e.sh` still passes with the modified `submit` tool.
- Go helper changes pass formatting and the required `make build`.

Deliver the work in two PRs: first the `docker/devnet/` runtime, the Compose example, and the `local_devnet/submit` changes; second the devnet workflow, lint wiring, Dependabot entry, and release documentation. 
Keep each under the repository's 700-line PR limit.

Automatic publication applies only to future tags that contain the workflow. Historical releases are not backfilled.

This file is a planning note. Repository rules keep planning notes in `docs/plans/` and never commit them; move it there or delete it before opening the PR.
