# Plan

1. Build the current checkout's v10 standalone app, Fibre server, and a small
   submission command into one Docker image.
2. Initialize one shared genesis with two equal-stake validators and a separate
   funded test account. Persist keys and chain data in a Compose volume.
3. Start both validators with RPC, REST API, and gRPC enabled. Start one Fibre
   server per validator, using its private signing endpoint.
4. After the chain and servers are ready, register both Fibre hosts and fund the
   test account's escrow. Wait for successful transaction execution.
5. Provide PFB/PFF scripts with transaction count, blob size, and interval flags.
   Use the existing Go clients and confirm each transaction. Support verifying
   PFF data by downloading it again.
6. Test from an empty volume, check both validators and public endpoints, submit
   multiple PFBs/PFFs, verify downloads, then stop/start and repeat.

## Evaluation

This is a light tooling change contained in `local_devnet`; no application or
protocol changes are needed. One image avoids mismatched binaries. Compose
health checks and committed transaction checks make startup failures visible.
Registration must happen after consensus starts because it is a transaction.
A finite submission tool is needed because the existing Fibre tools either
hard-code a single-validator setup or run by duration rather than count.

Keep the chain ID, validator count, funding, and port mapping fixed. Expose only
submission controls needed by tests. Use Docker DNS for Fibre discovery; scripts
run inside Compose, so host DNS changes are unnecessary. Document host-client
DNS setup separately. Both validators are needed for the chain to make progress.

## Validation

Tested on Docker Desktop, Linux arm64 containers, with 8 GB RAM:

- Fresh-volume and retained-volume e2e runs passed. Each committed four PFBs
  and four PFFs (1 KiB and 256 KiB), with every PFF download verified.
- Restart preserved account and genesis fingerprints. Setup did not repeat
  registration or escrow deposits. Both validators and both Fibre servers
  remained healthy; host RPC and API checks passed.
- `make build`, targeted Go lint and vet, Compose validation, and Bash syntax
  checks passed.

Startup testing identified and resolved shared-image build races, the Docker
kernel's missing BBR support, genesis transaction fees, and the CLI's non-JSON
output for an empty provider registry.
