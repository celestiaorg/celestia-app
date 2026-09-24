# Graviton throughput benchmark archive

See the [optimization inventory](OPTIMIZATIONS.md) for inherited work, benchmark additions, source links and final deployment status.

The final 120-validator run measured **383.295 GB/s of raw bytes represented by unique, successfully indexed PFF transactions** over 143.412 seconds. This is a synthetic benchmark with reused preencoded data and deliberately relaxed PFF validation, not a production-security or unique-data-ingestion result.

- [Experiment report](EXPERIMENT-REPORT.md): results, progression, failures, remaining limitations, and interpretation for the team.
- [Reproduction instructions](REPRODUCE.md): infrastructure, exact settings, binaries, deployment order, measurement method, and preservation requirements for agents/operators.

The evidence paths in these documents are relative to the deployment evidence bundle root, originally named `graviton-100-deployment`. Keep that bundle alongside these documents. They are artifact identifiers, not public download URLs. Source is on [integrate/fibrrrrrr-preston-graviton](https://github.com/celestiaorg/celestia-app/tree/integrate/fibrrrrrr-preston-graviton). Documentation is portable; private inventory, signing keys and IAM credentials must not be published.

The latest run is `load/final-run`; its contemporary result JSON is the authority for reported counts. Earlier `STATE.md` and `HANDOVER.md` describe historical states and must not override the final manifests. Live-capture and monitoring archive manifests, when present, record precisely what was rescued before teardown. Do not infer that a complete raw-block archive exists: measured heights had already been pruned when retrospective collection began.

See the [portable artifact index](ARTIFACTS.md) and [published final summary](final-run-summary.json).

## Evidence bundle index

`evidence/final/CAPTURE-INDEX.md` describes rescued artifacts; `evidence/final/SHA256SUMS-ALL` authenticates the local capture set. `evidence/final/history/` holds portable run summaries. `evidence/final/reproduction/` holds pinned manifests, public genesis/audits and `deployment-tools/` with the ramp runner, its dependencies and scanners. The helpers require inventory/path adaptation; they are not an automatic fresh-fleet installer. Live capture includes network settings, configuration and monitoring data; consult each capture manifest for coverage and failures.
