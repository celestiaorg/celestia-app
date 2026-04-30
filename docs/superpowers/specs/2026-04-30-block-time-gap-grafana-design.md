# Block Time Gap Grafana Panel — Design

**Linear:** [PROTOCO-1654](https://linear.app/celestia/issue/PROTOCO-1654/measure-longest-block-time-gap-on-mainnet-add-grafana-dashboard) · related: [PROTOCO-1363](https://linear.app/celestia/issue/PROTOCO-1363)
**OKR:** OKR1 KR1 — 0 network-wide downtime on Mainnet (incl. Fibre)
**Status:** Approved 2026-04-30

## Problem

OKR1 KR1 currently lacks continuous monitoring. There is no Grafana panel that surfaces "longest block-time gap" on Mainnet, and no alert tied to the OKR's 1-minute breach threshold. The existing `Block Interval` panel in `validator-complete.json` plots a `histogram_quantile(0.99, …[5m])` line — a smoothed p99 that dilutes single long gaps into the surrounding average and never trips a visible threshold for an isolated 60-second outage.

## Goals

1. A Grafana panel that surfaces block-time gaps clearly enough that a 60-second gap is visually obvious, with a green band marking the SLO-healthy region (≤6s) and a red band marking the OKR breach region (≥60s).
2. An alert that fires whenever block height fails to advance for ≥1 minute on Mainnet.
3. A scoping artifact (this document) that closes out the prerequisites tracked by PROTOCO-1363.

## Non-goals

- Per-block precision. Block interval is exposed only as a Prometheus histogram, so the panel uses histogram-derived series rather than per-block samples. Adding a direct gauge metric (e.g., `cometbft_consensus_last_block_interval_seconds`) is a worthwhile follow-up but out of scope here.
- Backfilling historical "longest gap" data prior to deployment.
- Tuning the existing `MainnetBlockIntervalHigh` (avg > 20s [5m]) alert. The new alert is additive.

## Design

### Panel: "Block Time Gap"

Two series:

- **Series A (smoothed average gap):** `max by (<grouping>) (rate(cometbft_consensus_block_interval_seconds_sum[1m]) / rate(cometbft_consensus_block_interval_seconds_count[1m]))`
- **Series B (1-minute breach indicator):** `max by (<grouping>) ((increase(cometbft_consensus_height[1m]) == bool 0) * 60)`

Series A reads as a smooth health line — typical mainnet values sit ~6s. Series B is a binary step function: it equals `60` whenever block height has not advanced for at least 1 minute, otherwise `0`. Series B is what makes a real OKR breach visually unmissable: when a true >1-minute gap occurs, the line jumps directly into the red zone at the 60-second threshold. Series A alone cannot do this — its 1-minute averaging dilutes a single 60s gap to ~11s, never crossing the red band.

The two series share an axis and a panel because they answer different questions about the same underlying signal: "is the network producing blocks at the expected rate" (A) and "is the OKR being breached right now" (B).

`<grouping>` varies by host dashboard:

| Dashboard | File | Grouping |
|---|---|---|
| Cross-network rollup | `infrastructure: ansible/monitoring/dashboards/grafana-central/network-overview.json` | `network` |
| Mainnet validator drilldown | `infrastructure: ansible/monitoring/dashboards/mainnet/validator-complete.json` | `instance` (scoped by `$cometbft_job`, `$cometbft_instance`) |
| Local docker stack | `celestia-app: observability/docker/grafana/dashboards/celestia.json` | `node_id` (scoped by `job="consensus-nodes"`, `node_id=~"$node"`) |

### Visual styling

- Y-axis: unit `s`, linear, min 0, no max.
- Threshold steps (absolute mode):
  - `green` from 0
  - `transparent` from 6
  - `red` from 60
- `thresholdsStyle.mode = "dashed+area"` — dashed lines at 6s and 60s, green area below 6s, red area above 60s.
- Default time range follows the dashboard's existing time picker.

### Companion stat panel: "Breach Minutes (last 24h)"

Single-number readout for OKR check-ins. Counts 1-minute samples in the last 24 hours where Mainnet height did not advance:

```promql
sum_over_time(
  (increase(cometbft_consensus_height{network="mainnet"}[1m]) == bool 0)[24h:1m]
)
```

Threshold steps: `green` from 0, `red` from 1. The panel reads `0` on a healthy day and turns red the instant any breach occurs in the rolling window. This is more directly OKR-meaningful than a "longest gap" number derived from histogram averages, which would be diluted by surrounding fast blocks.

### Alert: `MainnetBlockGapOneMinute`

Added to both copies of the mainnet alert rules in `celestiaorg/infrastructure`:

- `ansible/monitoring/alert-rules/mainnet-alerts.yml`
- `ansible/common/roles/prometheus/files/alert-rules/mainnet-alerts.yml`

```yaml
- alert: MainnetBlockGapOneMinute
  expr: increase(cometbft_consensus_height{network="mainnet"}[1m]) == 0
  labels:
    severity: warning
    okr: okr1-kr1
  annotations:
    summary: "Mainnet block gap exceeded 1 minute"
    description: "No block height advance on mainnet for at least 1 minute. OKR1 KR1 threshold breached."
    runbook_url: "https://github.com/celestiaorg/infrastructure/blob/main/ansible/monitoring/alert-rules/README.md"
```

`increase(_height[1m]) == 0` is the same idiom as the existing `MainnetNoBlocksProgress` rule, at a tighter window. No `for:` clause: the breach is the OKR threshold itself, no debouncing needed.

## Rationale

### Why a histogram-derived series instead of a new metric

CometBFT's existing `cometbft_consensus_block_interval_seconds` histogram is the only block-timing signal currently scraped on Mainnet. A new `_last` gauge in the celestia-core fork would be more accurate (per-block precision) but requires a fork PR, a celestia-app version bump, a Mainnet validator-set rollout, and a metric-name coordination across consumers. Series A + Series B together cover the OKR question with zero upstream changes.

### Why two series instead of one

Series A alone hides isolated long gaps: a 1-minute average over (9 × 6s) + (1 × 60s) = 11.4s, well below the 60s red threshold. Series A would never visually surface a true OKR breach. Series B alone is binary and uninformative during normal operation. Together: A is the everyday read, B is the breach detector that pops the line into the red zone the moment the OKR is violated.

### Why `increase(height) == bool 0` instead of histogram-based queries for Series B

The cometbft `block_interval_seconds` histogram uses default Prometheus buckets (largest finite bucket = 10s). Any block taking >10s lands in the `le="+Inf"` bucket, so `histogram_quantile(1.0, …)` returns `+Inf` precisely when a real breach occurs — Grafana renders it as no-value. Using `increase(height)` sidesteps the histogram entirely: it is a direct counter, has no buckets, and the `== bool 0` form (with the `bool` modifier) returns 1/0 cleanly so the multiplication produces a stable 0-or-60 step function.

### Why `increase(height) == 0` for the alert instead of bucket arithmetic

Three reasons: (1) it is the literal expression of the OKR statement, (2) it matches the idiom of the surrounding alert rules, (3) it survives metric renames or bucket-boundary changes upstream.

## Rollout

1. Land the celestia-app PR (panel mirror in local docker stack + this design doc).
2. Land the infrastructure PR (panels in two dashboards + alert rule in two YAML files).
3. Ansible run on the Grafana / Prometheus hosts picks up the dashboard and rule changes.
4. Validate in mainnet Grafana that the panel renders and that an alert preview confirms the rule loads.
5. Comment on PROTOCO-1654 with PR links; close PROTOCO-1363 prerequisites.

## Risks

- **Series A dilution is intentional, not a bug.** A 1-minute average over a single 60s gap surrounded by normal blocks reads ~11s, not 60s. Readers must understand that Series A is a health line, not a max-gap line. The panel description and Series B exist to make this unambiguous.
- **Series B latency.** `increase(_height[1m]) == bool 0` requires `_height` to have been stable for the full 1-minute lookback before evaluating to 1. Combined with the alert's `for: 0s`, the breach surfaces in the panel and fires the alert at the same wall-clock moment.
- **Network-overview cardinality.** `max by (network)` keeps cardinality low. No Prometheus risk.
- **Rule duplication.** The `mainnet-alerts.yml` file exists in two locations in the infrastructure repo. Both must be updated together; manual diff check after the PR lands is prudent.
