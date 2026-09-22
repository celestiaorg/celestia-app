#!/usr/bin/env python3
"""Capture existing Prometheus metrics for an explicitly timed benchmark phase."""
import argparse
import base64
import datetime
import json
import pathlib
import time
import urllib.parse
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('--phase', required=True)
parser.add_argument('--start-utc', required=True)
parser.add_argument('--duration', type=int, default=600)
parser.add_argument('--out', type=pathlib.Path, required=True)
parser.add_argument('--grafana', default='http://localhost:3000')
parser.add_argument('--password-file', required=True)
parser.add_argument('--username', default='admin')
parser.add_argument('--chain', required=True)
parser.add_argument('--components', required=True, help='Encoder component ID regular expression')
parser.add_argument('--data-mount', required=True)
args = parser.parse_args()
if not 1 <= args.duration <= 900:
    parser.error('duration must be 1..900 seconds')
start = datetime.datetime.fromisoformat(args.start_utc.replace('Z', '+00:00'))
if start.tzinfo is None:
    parser.error('start-utc must include timezone')
start = start.timestamp()
if args.out.exists():
    parser.error('output already exists; choose a new phase artifact')
args.out.parent.mkdir(parents=True, exist_ok=True)
password = pathlib.Path(args.password_file).expanduser().read_text().strip()
headers = {'Authorization': 'Basic ' + base64.b64encode((args.username + ':' + password).encode()).decode()}
chain = 'chain_id=' + json.dumps(args.chain)
selector = chain + ',component_id=~' + json.dumps(args.components)
queries = {
    'encoder': '{' + selector + ',job="workload",__name__=~"fibre_bench_raw_bytes_total|fibre_encoder_admitted_inputs_total|fibre_encoder_dispatched_.*|fibre_encoder_dispatch_batch_.*|fibre_encoder_ledger_.*|fibre_encoder_put_.*|fibre_encoder_heap_bytes|fibre_encoder_gc_.*"}',
    'identity': 'fibre_bench_identity_info{' + selector + '}',
    'host': '{' + selector + ',__name__=~"fibre_encoder_(process_resident_memory_bytes|cgroup_memory_.*|ena_.*|host_.*)|node_memory_MemAvailable_bytes"}',
    'cpu': '1-avg by(component_id)(rate(node_cpu_seconds_total{' + selector + ',mode="idle"}[1m]))',
    'up': 'up{' + chain + '}',
    'disk_free': 'node_filesystem_avail_bytes{' + chain + ',mountpoint=' + json.dumps(args.data_mount) + '}',
    'provider': '{' + chain + ',__name__=~"fibre_server_(upload_shard_bytes_total|upload_shard_in_flight|prune_entries_total|prune_duration_seconds_count)"}',
    'sample_age': 'max(time()-timestamp(fibre_encoder_admitted_inputs_total{' + selector + '}))',
}
with args.out.open('x') as output:
    output.write(json.dumps({'phase': args.phase, 'start_epoch': start, 'duration': args.duration, 'queries': queries}) + '\n')
    offsets = list(range(0, args.duration, 10)) + [args.duration]
    for offset in offsets:
        at = start + offset
        while time.time() < at + 10:
            time.sleep(min(1, at + 10 - time.time()))
        row = {'phase': args.phase, 'at_epoch': at, 'queries': {}}
        for name, query in queries.items():
            url = args.grafana + '/api/datasources/proxy/uid/prometheus/api/v1/query?' + urllib.parse.urlencode({'query': query, 'time': at})
            try:
                with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=10) as response:
                    row['queries'][name] = json.load(response)
            except Exception as exc:
                row['queries'][name] = {'error_type': type(exc).__name__}
        output.write(json.dumps(row) + '\n')
        output.flush()
