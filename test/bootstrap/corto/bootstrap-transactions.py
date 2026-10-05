#!/usr/bin/env python3
"""Register one validator or fund one encoder's escrow accounts, with a durable journal."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", choices=["register", "deposit"])
    p.add_argument("--binary", default="/usr/local/bin/celestia-appd")
    p.add_argument("--home", type=Path, required=True)
    p.add_argument("--host", required=True)
    p.add_argument("--node", required=True, help="http://validator-private-ip:26657")
    p.add_argument("--manifest", type=Path, required=True)
    p.add_argument("--journal", type=Path, required=True)
    p.add_argument("--provider", help="validator-private-ip:7980 for registration")
    p.add_argument("--execute", action="store_true", help="broadcast authorized bootstrap transactions")
    a = p.parse_args()
    os.umask(0o077)
    manifest = json.loads(a.manifest.read_text())
    accounts = [x for x in manifest["accounts"] if x["host"] == a.host]
    assert len(accounts) == (1 if a.mode == "register" else 128)
    if a.mode == "register" and not a.provider:
        p.error("registration requires --provider")
    if not a.execute:
        print(f"Prepared {len(accounts)} {a.mode} transactions; use --execute to submit.")
        return
    status = json.load(urllib.request.urlopen(a.node.rstrip("/") + "/status", timeout=10))["result"]
    if status["node_info"]["network"] != "corto-10" or status["sync_info"]["catching_up"]:
        raise RuntimeError("node must be synced to corto-10")
    a.journal.parent.mkdir(parents=True, exist_ok=True)
    lock = a.journal.with_suffix(".lock")
    fd = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(fd)
    journal = json.loads(a.journal.read_text()) if a.journal.exists() else {}

    def save():
        temporary = a.journal.with_suffix(".tmp")
        with temporary.open("w") as f:
            json.dump(journal, f, indent=2)
            f.flush()
            os.fsync(f.fileno())
        temporary.replace(a.journal)

    def cli(words, check=True):
        r = subprocess.run([a.binary, *words, "--home", str(a.home), "--node", a.node,
                            "--output", "json"], capture_output=True, text=True, timeout=60)
        if r.returncode:
            if not check:
                return None
            raise RuntimeError("SDK command failed; no automatic transaction retry")
        return json.loads(r.stdout)

    def confirm(txhash):
        until = time.monotonic() + 180
        while time.monotonic() < until:
            tx = cli(["query", "tx", txhash], check=False)
            if tx is not None:
                if int(tx.get("code", 0)) != 0:
                    raise RuntimeError(f"transaction {txhash} committed with failure")
                return tx
            time.sleep(1)
        raise RuntimeError(f"unresolved transaction {txhash}; reconcile before retry")

    def verify_state(address):
        if a.mode == "deposit":
            escrow = cli(["query", "fibre", "escrow-account", address])
            if not escrow.get("found") or int(escrow["escrow_account"]["balance"]["amount"]) != 500000000000:
                raise RuntimeError("committed escrow deposit did not yield expected balance")
        else:
            providers = cli(["query", "valaddr", "providers"])
            if not any(v["info"]["host"] == a.provider for v in providers.get("providers", [])):
                raise RuntimeError("committed registration absent from bonded provider list")

    try:
        for account in sorted(accounts, key=lambda x: x["key_name"]):
            address = account["address"]
            previous = journal.get(address)
            if previous:
                if previous["state"] == "confirmed":
                    continue
                if previous.get("txhash"):
                    confirm(previous["txhash"])
                    verify_state(address)
                    previous["state"] = "confirmed"
                    save()
                    continue
                raise RuntimeError(f"ambiguous earlier submission for {address}; manual reconciliation required")
            if a.mode == "deposit":
                escrow = cli(["query", "fibre", "escrow-account", address])
                if escrow.get("found"):
                    balance = int(escrow["escrow_account"]["balance"]["amount"])
                    if balance != 500000000000:
                        raise RuntimeError("existing non-target escrow balance requires reconciliation")
                    journal[address] = {"state": "confirmed", "source": "existing_escrow"}
                    save()
                    continue
                tx = ["tx", "fibre", "deposit-to-escrow", "500000000000utia"]
            else:
                tx = ["tx", "valaddr", "set-host", a.provider]
            journal[address] = {"state": "submission_started", "key_name": account["key_name"]}
            save()
            result = cli(tx + ["--from", account["key_name"], "--keyring-backend", "test",
                              "--chain-id", "corto-10", "--gas", "1000000", "--fees", "1utia",
                              "--broadcast-mode", "sync", "--yes"])
            if int(result.get("code", 0)) != 0 or not result.get("txhash"):
                journal[address]["state"] = "rejected_or_unknown"
                save()
                raise RuntimeError("CheckTx rejected or response incomplete; no automatic retry")
            journal[address]["txhash"] = result["txhash"]
            save()
            confirm(result["txhash"])
            verify_state(address)
            journal[address]["state"] = "confirmed"
            save()
        print(f"Confirmed {len(accounts)} {a.mode} operations for {a.host}.")
    finally:
        lock.unlink()


if __name__ == "__main__":
    main()
