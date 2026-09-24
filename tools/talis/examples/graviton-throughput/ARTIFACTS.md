# Artifact index

The [report](EXPERIMENT-REPORT.md), [reproduction guide](REPRODUCE.md), [archive overview](THROUGHPUT-RUNBOOK.md) and [final run summary](final-run-summary.json) are published here. The JSON retains the transaction-deduplicated measurements and accounting limitations, with wall-clock timestamps omitted.

[Average and rolling peak calculations](throughput-windows.json) include per-block deduplicated raw-byte aggregates for recomputation.

Detailed evidence lives in a separately retained deployment bundle, originally `graviton-100-deployment`. Paths below are relative to that bundle, not downloadable repository files. `evidence/final` and `load/final-run` are neutral aliases for its final capture and run directories. Raw evidence retains its original timestamps; published experiment descriptions omit wall-clock scheduling. Obtain the bundle from its operator before reproducing the analysis.

| Bundle path | Purpose |
| --- | --- |
| `evidence/final/CAPTURE-INDEX.md` | Capture coverage and gaps |
| `evidence/final/SHA256SUMS-ALL` | File integrity checksums |
| `evidence/final/performance/` | Indexed receipts, identity audit and measured results |
| `evidence/final/monitoring/` | Metrics, query definitions and dashboard exports |
| `evidence/final/network/` | NIC, routing, kernel and connection captures |
| `evidence/final/history/` | Earlier experiment summaries |
| `evidence/final/reproduction/` | Pinned manifests, genesis audits and adapted orchestration references |
| `fixes/latest-branch-release/83f2e81c5/` | Hash-verified ARM binaries and build metadata |
| `load/final-run/` | Contemporary final-run accounting |

The internal bundle contains infrastructure addresses and identifiers; do not publish it wholesale. Signing keys, keyrings and IAM credentials are excluded. Use newly generated identities for a new experiment. The measured raw blocks were already pruned during retrospective capture; preserved indexed receipts and contemporary scans do not constitute a complete raw-block archive.
