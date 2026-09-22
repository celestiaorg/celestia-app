# Fleet dashboards

The encoder, service and fullscale-readiness dashboards preserve the aggregate
and individual views used by the benchmark monitor. The existing Grafana file
provider loads these JSON files. They use datasource UID `prometheus`.

Select the chain, encoder/run and provider/host filters before comparing results.
The fullscale dashboard also requires selecting the data filesystem. There is no
fixed host inventory, throughput target or default chain in these files.

Required labels are `chain_id`, `component_id`, `role` and, for run identity,
`run_id`. Component IDs must be unique within the selected chain. Aggregate
queries restrict encoder sources to job `workload` and provider sources to
`freshchain-fibre-central`; configure matching scrape jobs or adapt these selectors.
Host panels use node-exporter metrics. Custom encoder RSS/cgroup/ENA metrics need
a separately installed host sampler; node-exporter process RSS is not encoder RSS.

The original campaign panels on the encoder dashboard additionally require
`fibre_bench_expected_component` and `fibre_bench_component_covered` from a campaign
observer. They remain empty without that coverage. The direct-source aggregate
and per-encoder panels do not require campaign coverage. Missing data is unknown,
not zero traffic or proof of success.

Rates handle counter resets but are estimates. Identity joins use current identity
and cannot isolate history across a process/run change. Compare a time range wholly
inside a run and reconcile exact client receipts separately. Input size panels show
means, not percentiles. Confirmed/unknown input totals assume the memory ledger's
monotonic terminal accounting; a different exporter must preserve that contract.
Unknown outcomes do not prove failure on-chain.

Raw encoder bytes, assembled blob bytes, provider shard payload and physical NIC
bytes describe different boundaries. Provider shard counters include duplicates
and cannot attribute traffic to selected encoders. Histogram quantiles are bounded
by exported buckets; a top-bucket value is not an exact tail latency.

`tools/fibre-native-observer/observe.py` records aligned source samples without
starting load or changing services. It requires an explicit chain, encoder selector,
data mount, start time, phase ID and credential-file path. Output must be new.
For example, from the repository root:

```sh
python3 tools/fibre-native-observer/observe.py \
  --phase example --start-utc 2026-01-01T00:00:00Z --duration 600 \
  --chain example-chain --components 'encoder-.*' --data-mount /data \
  --password-file /protected/grafana-password --out /tmp/example-prom.jsonl
```

The observer preserves collection errors rather than replacing them with zeros.
It is evidence collection, not a watchdog: load owners must provide graceful stop,
drain and exact-ID accounting independently. Credentials are never written to output.
