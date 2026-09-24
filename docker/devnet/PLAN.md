# Publish reusable Celestia devnet images

## Goal and scope

Provide a reliable local Celestia network that developers can obtain as published images and start with a small Compose file. No repository checkout or Go installation should be required.

Run exactly two containers:

- **Validator:** runs both `celestia-appd` and `fibre`.
- **Bridge:** runs `celestia bridge`.

Version 1 provides one consensus validator, ten fixed public test accounts, Fibre support, and persistent state. Keep the existing `local_devnet` example available.

For v1, build and publish both devnet images from `celestia-app`. Move bridge devnet packaging and publication to `celestia-node` as a follow-up, using the same release-driven publishing rules and preserving the runtime and credential interface. After that move, `celestia-app` consumes a pinned bridge devnet image and continues testing the validator/bridge pair. Consumers should only need to update the bridge image reference.

Downstream migrations and consumer test suites are out of scope. Authenticated bridge RPC, light nodes, configurable validator counts, supplied account keys, configurable account counts, and specialized eviction-test settings are follow-up work.

Relevant existing implementations:

- [Sovereign image build and publication](https://github.com/Sovereign-Labs/sovereign-sdk/blob/3208eb5c54243a1854f44a453787335dc9f87642/docker/Makefile#L73-L87) and [Testcontainers integration](https://github.com/Sovereign-Labs/sovereign-sdk/blob/3208eb5c54243a1854f44a453787335dc9f87642/crates/adapters/celestia/src/test_helper/docker.rs#L24-L83).
- [Lumina topology](https://github.com/celestiaorg/lumina/blob/cb5706332716c50a6d6f795db20c1708c607c5c7/ci/docker-compose.yml#L1-L170) and [validator startup](https://github.com/celestiaorg/lumina/blob/cb5706332716c50a6d6f795db20c1708c607c5c7/ci/run-validator.sh#L119-L245), including genesis-funded escrow and colocated Fibre.
- [`local_devnet` image](../../local_devnet/Dockerfile#L8-L17) and [topology](../../local_devnet/compose.yaml#L30-L90): one image supplies two validator containers, two Fibre containers, and separate initialization and provisioning jobs.

## Images and runtime

Publish these images for `linux/amd64` and `linux/arm64` under the same app release tag:

- `ghcr.io/celestiaorg/celestia-devnet-validator:<app-tag>`
- `ghcr.io/celestiaorg/celestia-devnet-bridge:<app-tag>`

Build the validator and Fibre binaries from the tagged app checkout. Pin the bridge's celestia-node dependency by tag and multi-platform digest in the Dockerfile. The digest fixes the exact image contents even if the tag moves. Weekly Dependabot updates must pass the devnet smoke suite before merging. Keep the literal digest out of this plan.

Provide a published-image Compose example and a source-build override. Initialization, readiness, and process management belong inside the images so they also work without Compose.

The validator startup script manages both processes. It must forward shutdown signals, wait for children to exit, and stop the container if either service fails. Perform Fibre host registration inside this container; do not add separate initialization, Fibre, or provisioning containers.

On first boot:

1. Import the fixed test account keys and create the validator genesis.
2. Fund accounts, initialize Fibre escrow, and fund the module account backing that escrow.
3. Start the validator and Fibre server.
4. Register the advertised Fibre address and verify that registration committed successfully.

Escrow supports [genesis initialization](../../x/fibre/keeper/genesis.go#L9-L15); host registration requires a transaction because [`valaddr` has no genesis initialization logic](../../x/valaddr/genesis.go#L25-L31). Do not bake initialized chain state into the images.

Bundle fixed disposable credentials in both images at `/credentials`. For `node-0` through `node-9`, provide `.key` files (armored, password `password`), `.plaintext-key` files (hex), and `.addr` files. Also provide `validator-0.key`, `validator-0.plaintext-key`, `validator-0.addr`, and `validator-0.valaddr`. These are ordinary files inside the containers. No shared writable credential volume is needed. Document how host clients copy these files.

The bridge uses `node-0` and obtains the first-block hash through validator RPC, without a shared genesis-hash volume.

Defaults:

| Setting | Default |
|---------|---------|
| Network and chain ID | `devnet` |
| Client accounts | Ten fixed public test accounts |
| Account funding | `1000000000000000utia` per account |
| Fibre escrow | `1000000000000utia` per funded account |
| Block timing | `1s` delayed precommit timeout |
| Fibre advertised address | `localhost:7980` |
| Bridge RPC authentication | Disabled |

Retain network, block timing, Fibre advertised-address, and bridge connection settings. Use binary defaults for block size, square size, and mempool TTL.

Bind published ports to loopback. Keep validator signing internal and use explicit `127.0.0.1` addresses between the colocated processes. Document container-client addressing separately.

Validator readiness requires block production, a listening Fibre server, and committed host registration. Bridge readiness requires a usable RPC endpoint and synchronized headers. Bound all startup waits and report failures in container logs.

Persist validator and Fibre state in the validator container's data volume, and bridge state in its own volume. Restart preserves state; deleting volumes resets it. Incompatible version changes require a reset. Fixed account identities survive resets.

## CI and publication

Keep publication tied to future app release tags, including prereleases. Devnet-only fixes wait for an app release. Do not backfill historical releases.

Use one dedicated workflow:

1. Build both images and run self-contained smoke tests on native amd64 and arm64 runners.
2. Publish the exact tested images on upstream `v*` tags or manual reruns targeting an existing tag containing the devnet implementation. Do not rebuild after testing.
3. Assemble and verify both multi-platform manifests after both architectures pass.

Run validation for pull requests touching `docker/devnet/**`, `.github/workflows/devnet.yml`, or `local_devnet/submit/**`, all merge-queue entries, release tags, and manual dispatch. Preserve failure logs and confine registry credentials and package-write permissions to publication jobs.

Publish publicly pullable GHCR packages, with app-version and per-architecture tags (`<app-tag>-amd64` and `<app-tag>-arm64`). Do not add Docker Hub publication, `latest`, `main`, SHA aliases, or independent devnet revision tags. Leave production image publication unchanged.

Update release documentation with image names, usage, architecture verification, and the manual-rerun procedure.

## Acceptance and delivery

Verify on both architectures:

- Fresh startup produces blocks, registers Fibre, and synchronizes the bridge.
- All fixed accounts have the expected balances and escrow.
- Ordinary and Fibre blobs complete roundtrips with matching contents.
- Restart preserves chain progress and previously stored blobs.
- Reset creates fresh state with the same fixed account identities.
- Shutdown terminates both validator processes; failure of either stops the container.
- Failed registration and unavailable dependencies produce bounded startup failures.
- Two isolated stacks can run concurrently.
- Documented connection and timing overrides work.

Record startup time with cached images without committing to an unmeasured performance target. Do not add Sovereign or Lumina test suites to these acceptance criteria.

Run applicable script, Dockerfile, Compose, workflow, and documentation checks. If existing Go helpers change, run formatting, `make build`, and their relevant tests, including the existing local-devnet checks. Before opening a PR modifying Go code, run `make lint` and `make test-short`.

Deliver runtime, Compose, and smoke tests first; publication automation and release documentation second. Keep each PR under the repository's 700-line limit.

Keep this planning note out of implementation PRs.
