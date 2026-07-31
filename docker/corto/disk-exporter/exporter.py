#!/usr/bin/env python3
"""Prometheus exporter for celestia-app database directory sizes."""

import http.server
import os

DATA_DIR = "/data/data"
PORT = 9101

DBS = ["application.db", "blockstore.db", "state.db", "evidence.db", "snapshots", "cs.wal"]


def dir_size(path):
    total = 0
    for dirpath, _, filenames in os.walk(path):
        for f in filenames:
            fp = os.path.join(dirpath, f)
            try:
                total += os.path.getsize(fp)
            except OSError:
                pass
    return total


class MetricsHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        lines = [
            "# HELP celestia_db_size_bytes Size of each database directory in bytes.",
            "# TYPE celestia_db_size_bytes gauge",
        ]
        for db in DBS:
            path = os.path.join(DATA_DIR, db)
            if os.path.isdir(path):
                size = dir_size(path)
                lines.append(f'celestia_db_size_bytes{{db="{db}"}} {size}')
        lines.append("")
        lines.append(
            "# HELP celestia_data_dir_size_bytes Total size of the data directory in bytes."
        )
        lines.append("# TYPE celestia_data_dir_size_bytes gauge")
        lines.append(f"celestia_data_dir_size_bytes {dir_size(DATA_DIR)}")

        body = "\n".join(lines) + "\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.end_headers()
        self.wfile.write(body.encode())

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    server = http.server.HTTPServer(("", PORT), MetricsHandler)
    server.serve_forever()
