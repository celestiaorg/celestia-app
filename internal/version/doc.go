// Package version reports the binary version of active validators over OTLP/HTTP.
//
// Enable reporting in config/app.toml:
//
//	[version-reporting]
//	endpoint = "https://collector.example.com"
//	interval = "1m"
//
// An empty endpoint disables reporting. The endpoint is a base URL; /v1/metrics
// is appended while preserving any path prefix. The interval defaults to one
// minute. Run celestia-appd config sync to add these settings to an existing file.
//
// Eligibility is checked at startup and on each interval using core's local
// status. Reports are sent only when the node is caught up and its consensus
// address has positive voting power in the active set. Membership changes take
// effect without a restart. Collector failures do not stop the node; the next
// interval retries. Reporting uses a private metrics provider, independent of
// --otel-endpoint and telemetry.enabled.
//
// Each report contains celestia_validator_build_info (value 1) and
// celestia_validator_report_timestamp_seconds (report-generation Unix time).
// Both carry chain_id, consensus_address, version, and commit labels. Version
// and commit identify the outer celestia-appd executable in multiplexer builds.
//
// Consumers must account for self-reported values, node clock skew, stale
// reports, and active validators that have not reported. Validator names and
// current membership can be resolved externally through staking and consensus
// queries, joining reports by (chain_id, consensus_address).
package version
