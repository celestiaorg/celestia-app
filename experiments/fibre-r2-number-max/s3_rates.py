#!/usr/bin/env python3
"""Reports successful S3 object bytes per validator in a time window.

Usage: s3_rates.py <bucket> <chain-id> <start-unix> <end-unix> <validator>...
"""
import json
import subprocess
import sys
from datetime import datetime


def objects(bucket, prefix):
    out = subprocess.run(
        ["aws", "s3api", "list-objects-v2", "--bucket", bucket, "--prefix", prefix,
         "--query", "Contents[].[LastModified,Size]", "--output", "json"],
        check=True, capture_output=True, text=True).stdout
    return [(int(datetime.fromisoformat(t).timestamp()), s) for t, s in json.loads(out) or []]


def main():
    bucket, chain, start, end = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
    total = 0
    for val in sys.argv[5:]:
        objs = [(t, s) for t, s in objects(bucket, f"{chain}/{val}/") if start <= t < end]
        nbytes = sum(s for _, s in objs)
        total += nbytes
        per_sec = [0] * (end - start)
        for t, s in objs:
            per_sec[t - start] += s
        rolling = [sum(per_sec[i:i + 60]) / 60 for i in range(max(1, len(per_sec) - 59))]
        print(f"{val}: {len(objs)} objects, {nbytes / 1e9:.1f} GB, avg {nbytes / (end - start) / 1e9:.2f} GB/s, "
              f"min rolling-60s {min(rolling) / 1e9:.2f} GB/s")
    print(f"aggregate: {total / 1e9:.1f} GB, avg {total / (end - start) / 1e9:.2f} GB/s over {end - start} s")


if __name__ == "__main__":
    main()
