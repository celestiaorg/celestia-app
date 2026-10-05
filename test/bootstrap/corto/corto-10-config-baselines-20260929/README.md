# Corto release default configuration baseline

Public defaults generated locally from the checksum-verified published `v10.3.0-corto` Darwin ARM64 standalone binary. The CLI reports release commit `861c71c845e155ab8bdbe49a1d1453bdb4a3ccd1`. See `release-source-manifest.json` for sources, checksums, normalization and reproduction command.

- `release-module-genesis.json`: complete default module genesis (`app_state`), with JSON keys sorted.
- `release-consensus-params.json`: complete CLI-generated consensus parameters, with JSON keys sorted.
- `release-{app,config,client}.toml`: unchanged public configuration templates from the isolated initialization.
- `release-version.txt`: binary version and build dependencies.
- `release-asset-checksum.txt`: the matching published archive checksum entry.

These are release baselines, not approved deployment settings or a deployable genesis. They have no funded accounts or validator gentxs. No private key, node key, keyring or generated node identity is included. Template references to private-key filenames contain paths only, not key material. No live services or cloud resources were changed.

The CLI consensus baseline contains app version 10, block maximum 33,554,432 bytes, maximum gas -1, and core-default evidence settings (100,000 blocks / 48 hours / 1 MiB). Do not substitute `app.DefaultConsensusParams()` and assume it reproduces the CLI evidence settings.

Apply the separately documented deployment overrides explicitly, then compare normalized candidate and release manifests. Candidate public defaults and provenance are now captured in `candidate-*`; `default-parameter-diff.json` compares generated defaults. `deployment-module-template.json` and `deployment-consensus-params.json` apply approved parameter overrides to the release baseline; `deployment-versus-release-parameter-diff.json` lists those changes. These are still templates without funding, account addresses or gentxs, not a deployable genesis. Runtime/node/encoder changes are recorded separately in the runtime decisions.
