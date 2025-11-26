# Signing Latency

A tool to analyze signing latency metrics from Celestia validator logs. It parses a trace file containing signing latency data and calculates statistics (min, max, average, median) grouped by message type.

Note: the tracing latency file is only for remote KMS. For local signing, the validator will not generate the trace file.

## Usage

### Enable the signing latency traces

The signing latency traces file is generated automatically when the `signing_latency` tracing table is enabled. To enable it, update the `$CELESTIA_APP_HOME/config/config.toml` tracing section to be:

```toml
# The tracer to use for collecting trace data.
trace_type = "local"

# The list of tables that are updated when tracing. All available tables and
# their schema can be found in the pkg/trace/schema package. It is represented as a
# comma separate string. For example: "consensus_round_state,mempool_tx".
tracing_tables = "signing_latency"
```

If you have existing tracing tables, add the `signing_latency` table at the end.

Then restart the validator.

The traces should be in `$CELESTIA_APP_HOME/data/traces`.

Note: make sure to revert the `trace_type` to `noop` after you're done with the tracing, and restart the validator, to avoid the traces data folder growing indefinitely.

### Measure the results

The binary can then be used as follows:

```bash
go run ./tools/signing_latency <traces_file_path>
```

The trace file path is in `$CELESTIA_APP_HOME/data/traces/signing_latency.jsonl`. If you enabled tracing, this file should be there.

Make sure to run the tool on a copy of the `traces_file` to avoid any file corruption.

## Example

```bash
$ go run ./tools/signing_latency signing_latency.jsonl

prevote:
  count:   1250
  min:     2.15 ms
  max:     45.32 ms
  average: 9.97 ms
  median:  8.50 ms

precommit:
  count:   1250
  min:     1.89 ms
  max:     42.10 ms
  average: 8.45 ms
  median:  7.20 ms

compactblock:
  count:   625
  min:     5.20 ms
  max:     120.50 ms
  average: 25.30 ms
  median:  22.10 ms
```

## Traces Format

The tool expects JSON log entries with the following structure:

```json
{"chain_id":"test","node_id":"...","table":"signing_latency","timestamp":"...","msg":{"height":14,"round":0,"latency":9972917,"message_type":"prevote"}}
```

The `latency` field should be in nanoseconds and will be converted to milliseconds in the output.

## Acceptable values

TBD
