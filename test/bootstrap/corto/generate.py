#!/usr/bin/env python3
"""Prepare private SDK keys, then finalize corto-10 genesis with real host IPs."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import subprocess
from concurrent.futures import ThreadPoolExecutor

ROOT = Path(__file__).resolve().parents[3]
BASE = Path(__file__).resolve().parent / "corto-10-config-baselines-20260929"
SUPPLY = 150_000_000_000 * 1_000_000


def read(path):
    return json.loads(Path(path).read_text())


def write(path, value):
    Path(path).write_text(json.dumps(value, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=["prepare", "finalize"])
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--private-dir", required=True, type=Path)
    parser.add_argument("--public-dir", required=True, type=Path)
    parser.add_argument("--inventory", type=Path)
    parser.add_argument("--resume", action="store_true", help="resume private key preparation")
    args = parser.parse_args()
    os.umask(0o077)
    private = args.private_dir.resolve()
    public = args.public_dir.resolve()
    if private == ROOT or ROOT in private.parents:
        parser.error("private-dir must be outside the repository")
    if private == public or private in public.parents or public in private.parents:
        parser.error("private and public directories must be disjoint")
    if args.stage == "prepare":
        private.mkdir(mode=0o700, parents=True, exist_ok=args.resume)
        public.mkdir(parents=True, exist_ok=args.resume)
    if private.stat().st_mode & 0o077:
        parser.error("private-dir must have mode 0700")
    binary = args.binary.resolve()

    def run(*words, home):
        result = subprocess.run([str(binary), *map(str, words), "--home", str(home)],
                                capture_output=True, text=True, input="y\n" if "export" in words else None)
        if result.returncode:
            # CLI output can contain private material; never echo it.
            (private / "last-error.log").write_text(result.stdout + result.stderr)
            raise RuntimeError("SDK command failed; inspect private last-error.log")
        return result.stdout.strip()

    if args.stage == "prepare":
        version = run("version", home=private / "treasury")
        manifest = {"chain_id": "corto-10", "cli_version": version,
                    "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                    "accounts": [], "validators": [], "encoders": []}

        def key(host, name, balance, active=False):
            home = private / host
            home.mkdir(mode=0o700, exist_ok=True)
            existing = subprocess.run([str(binary), "keys", "show", name, "--keyring-backend", "test",
                                       "--output", "json", "--home", str(home)], capture_output=True, text=True)
            if existing.returncode == 0:
                data = json.loads(existing.stdout)
            else:
                data = json.loads(run("keys", "add", name, "--keyring-backend", "test",
                                      "--no-backup", "--output", "json", home=home))
            account = {"host": host, "key_name": name, "address": data["address"],
                       "utia": str(balance), "active": active}
            manifest["accounts"].append(account)
            return account

        for i in range(10):
            host = f"validator-{i}"
            home = private / host
            if not (home / "config/genesis.json").exists():
                run("init", host, "--chain-id", "corto-10", home=home)
            account = key(host, "validator", 100_100 * 1_000_000)
            node_id = run("comet", "show-node-id", home=home)
            manifest["validators"].append({"host": host, "address": account["address"],
                                            "node_id": node_id})
        def encoder(i):
            host = f"encoder-{i}"
            for j in range(128):
                key(host, f"fibre-{j}", 1_000_000 * 1_000_000, j < 32)
            active = [run("keys", "export", f"fibre-{j}", "--unarmored-hex", "--unsafe",
                          "--keyring-backend", "test", home=private / host) for j in range(32)]
            if not all(len(k) == 64 and all(c in "0123456789abcdefABCDEF" for c in k) for k in active):
                raise RuntimeError("unexpected private key encoding")
            (private / host / "active-private-keys.hex").write_text("\n".join(active) + "\n")
            manifest["encoders"].append({"host": host, "funded_accounts": 128,
                                         "active_keys": [f"fibre-{j}" for j in range(32)],
                                         "later_escrow_utia": "500000000000"})
        with ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(encoder, range(10)))
        key("treasury", "treasury", 148_718_999_000 * 1_000_000)
        manifest["accounts"].sort(key=lambda a: (a["host"], a["key_name"]))
        manifest["encoders"].sort(key=lambda a: a["host"])
        accounts = manifest["accounts"]
        assert len(accounts) == 1291
        assert len({a["address"] for a in accounts}) == len(accounts)
        assert len({v["node_id"] for v in manifest["validators"]}) == 10
        assert sum(int(a["utia"]) for a in accounts) == SUPPLY
        write(public / "accounts.json", manifest)
        write(public / "allocations.json", [{"address": a["address"], "coins": [
            {"denom": "utia", "amount": a["utia"]}]} for a in accounts])
        primary = private / "validator-0"
        draft = read(primary / "config/genesis.json")
        draft["app_state"] = read(BASE / "deployment-module-template.json")
        draft["consensus"]["params"] = read(BASE / "deployment-consensus-params.json")
        write(primary / "config/genesis.json", draft)
        run("genesis", "bulk-add-genesis-account", public / "allocations.json", home=primary)
        run("genesis", "validate", home=primary)
        write(public / "funded-genesis-NOT-FINAL.json", read(primary / "config/genesis.json"))
        print("Prepared 1291 unique funded accounts; private keys remain outside repository.")
        return

    if not args.inventory:
        parser.error("finalize requires --inventory with real validator IPs")
    manifest = read(public / "accounts.json")
    if hashlib.sha256(binary.read_bytes()).hexdigest() != manifest["binary_sha256"]:
        parser.error("use the same SDK binary as prepare")
    inventory = read(args.inventory)
    expected = {f"validator-{i}" for i in range(10)}
    if set(inventory) != expected:
        parser.error("inventory must map exactly validator-0 through validator-9 to IP strings")
    ips = [ipaddress.ip_address(value) for value in inventory.values()]
    if len(set(ips)) != 10 or any(ip.version != 4 or ip.is_loopback or ip.is_unspecified or ip.is_multicast for ip in ips):
        parser.error("inventory needs ten distinct non-loopback host IPs")
    primary = private / "validator-0"
    if (public / "genesis.json").exists():
        parser.error("final genesis already exists; use a fresh preparation to regenerate")
    genesis = read(primary / "config/genesis.json")
    genesis["app_state"] = read(BASE / "deployment-module-template.json")
    genesis["consensus"]["params"] = read(BASE / "deployment-consensus-params.json")
    genesis["chain_id"] = "corto-10"
    assert genesis["consensus"]["params"]["version"]["app"] == "11"
    write(primary / "config/genesis.json", genesis)
    allocations = [{"address": a["address"], "coins": [{"denom": "utia", "amount": a["utia"]}]}
                   for a in manifest["accounts"]]
    write(private / "allocations.json", allocations)
    run("genesis", "bulk-add-genesis-account", private / "allocations.json", home=primary)
    funded = read(primary / "config/genesis.json")
    for validator in manifest["validators"]:
        host = validator["host"]
        home = private / host
        write(home / "config/genesis.json", funded)
        gentx = primary / "config/gentx" / f"{host}.json"
        gentx.parent.mkdir(exist_ok=True)
        run("genesis", "gentx", "validator", "100000000000utia", "--chain-id", "corto-10",
            "--ip", inventory[host], "--node-id", validator["node_id"],
            "--moniker", host, "--commission-rate", "0.1", "--commission-max-rate", "0.2",
            "--commission-max-change-rate", "0.01", "--min-self-delegation", "1",
            "--fees", "1utia", "--gas", "1000000",
            "--keyring-backend", "test", "--output-document", gentx, home=home)
    run("genesis", "collect-gentxs", home=primary)
    run("genesis", "validate", home=primary)
    final = read(primary / "config/genesis.json")
    txs = final["app_state"]["genutil"]["gen_txs"]
    assert len(txs) == 10
    assert len({tx["body"]["messages"][0]["pubkey"]["key"] for tx in txs}) == 10
    assert all(tx["body"]["messages"][0]["value"] == {"denom": "utia", "amount": "100000000000"} for tx in txs)
    assert all(tx["auth_info"]["fee"]["amount"] == [{"denom": "utia", "amount": "1"}]
               and tx["auth_info"]["fee"]["gas_limit"] == "1000000" for tx in txs)
    assert sum(int(c["amount"]) for a in final["app_state"]["bank"]["balances"] for c in a["coins"] if c["denom"] == "utia") == SUPPLY
    for validator in manifest["validators"]:
        home = private / validator["host"]
        write(home / "config/genesis.json", final)
    write(public / "genesis.json", final)
    write(public / "peers.json", {v["host"]: f'{v["node_id"]}@{inventory[v["host"]]}:26656' for v in manifest["validators"]})
    write(public / "validation.json", {"sdk_genesis_validate": "passed", "gentxs": 10,
          "unique_accounts": 1291, "supply_utia": str(SUPPLY),
          "genesis_sha256": hashlib.sha256((public / "genesis.json").read_bytes()).hexdigest()})
    print("Finalized and SDK-validated corto-10 genesis with ten equal validators.")


if __name__ == "__main__":
    main()
