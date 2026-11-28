#!/usr/bin/env bash
# test-orchestrate.sh: offline tests for plan-segments.sh, apply-checkpoints.sh and segment-run.sh.
# Uses stub binaries (gcloud backed by a local directory, an applier, celestia-appd, fsboot, fshdr) in a temp dir
# and the real ../segment.sh:
# no network, no cloud resources, no VMs. Run: tools/fastsync/orchestrate/test-orchestrate.sh
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
PASS=0 FAILS=0
ok() { PASS=$((PASS + 1)); echo "ok   $*"; }
bad() { FAILS=$((FAILS + 1)); echo "FAIL $*"; }
expect_eq() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1: got [$2] want [$3]"; fi; }
expect_has() { if grep -qF -- "$3" <<< "$2"; then ok "$1"; else bad "$1: output lacks [$3]"; printf '     | %s\n' "${2//$'\n'/$'\n'     | }"; fi; }
check() { local d=$1; shift; if "$@"; then ok "$d"; else bad "$d"; fi; }
expect_lacks() { if grep -qF -- "$3" <<< "$2"; then bad "$1: output has [$3]"; else ok "$1"; fi; }
expect_rc() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1: rc $2, want $3"; fi; }

PLAN=$HERE/plan-segments.sh APPLY=$HERE/apply-checkpoints.sh SEG=$HERE/segment-run.sh

echo "== plan-segments.sh"
out=$("$PLAN" --to 1000000 --every 250000 --vms 2 | grep -v '^#')
expect_eq "plan 1..1M on 2 VMs" "$out" "$(printf '%s\n' \
  $'seg\tstart_ckpt\tfirst_height\tend_height\tblocks\tvm\twave' \
  $'0\tgenesis\t1\t250000\t250000\t0\t0' \
  $'1\t250000\t250001\t500000\t250000\t1\t0' \
  $'2\t500000\t500001\t750000\t250000\t0\t1' \
  $'3\t750000\t750001\t1000000\t250000\t1\t1')"
expect_eq "checkpoints 1..1M" "$("$PLAN" --to 1000000 --format checkpoints)" "250000,500000,750000"
out=$("$PLAN" --from 300001 --to 760000 --vms 8 | awk 'NR>2 {print $2, $3, $4}')
expect_eq "plan from mid-range" "$out" "$(printf '%s\n' '300000 300001 500000' '500000 500001 750000' '750000 750001 760000')"
expect_eq "checkpoints from mid-range" "$("$PLAN" --from 300001 --to 760000 --format checkpoints)" "300000,500000,750000"
expect_eq "short range: one genesis segment" "$("$PLAN" --to 5 | awk 'NR>2 {print $2, $3, $4}')" "genesis 1 5"
expect_eq "short range: no checkpoints" "$("$PLAN" --to 5 --format checkpoints)" ""
expect_eq "header line" "$("$PLAN" --to 16000000 --vms 16 | head -1)" \
  "# plan: heights 1..16000000, checkpoint every 250000, 16 VM(s) -> 64 segment(s), 4 wave(s)"
out=$("$PLAN" --to 500001 --format commands --bucket gs://bkt/p --script /opt/seg.sh --run-args "--home /data/h --chain-id c")
expect_eq "commands" "$out" "$(printf '%s\n' \
  $'vm0\t/opt/seg.sh --bucket gs://bkt/p --checkpoint genesis --end 250000 --home /data/h --chain-id c' \
  $'vm0\t/opt/seg.sh --bucket gs://bkt/p --checkpoint 250000 --end 500000 --home /data/h --chain-id c' \
  $'vm0\t/opt/seg.sh --bucket gs://bkt/p --checkpoint 500000 --end 500001 --home /data/h --chain-id c')"
for args in "--to 5 --from 6" "--to 10 --vms 0" "--to x" "--to 10 --every 0" "--to 10 --format commands" \
  "--to 10 --format commands --bucket s3://x" "--to 10 --format commands --bucket gs://b/../x" "--to 10 --format bogus"; do
  set +e; read -r -a a <<< "$args"; "$PLAN" "${a[@]}" >/dev/null 2>&1; rc=$?; set -e
  check "rejects: $args" [ "$rc" != 0 ]
done

# ---- stubs
S=$T/stubs; mkdir -p "$S" "$T/bucket"
cat > "$S/gcloud" <<'EOF'
#!/usr/bin/env bash
# gcloud storage cp|ls backed by $STUB_BUCKET_ROOT (gs://NAME/... -> $STUB_BUCKET_ROOT/NAME/...)
set -euo pipefail
map() { case $1 in gs://*) echo "$STUB_BUCKET_ROOT/${1#gs://}";; *) echo "$1";; esac; }
[ "$1" = storage ] || exit 64
case $2 in
  ls) [ -e "$(map "$3")" ];;
  cp) shift 2; args=("$@"); dst=$(map "${args[-1]}"); unset 'args[-1]'
      for s in "${args[@]}"; do
        src=$(map "$s"); [ -f "$src" ] || { echo "stub gcloud: no such object $s" >&2; exit 1; }
        if [[ $dst == */ ]]; then mkdir -p "$dst"; cp "$src" "$dst"; else mkdir -p "$(dirname "$dst")"; cp "$src" "$dst"; fi
      done;;
  *) exit 64;;
esac
EOF
cat > "$S/cstool" <<'EOF'
#!/usr/bin/env bash
# cstool apply stub: "applies" up to -to by writing the height into application.db/HEIGHT
set -euo pipefail
[ "$1" = apply ] || exit 64; shift
while [ $# -gt 0 ]; do case $1 in -to) to=$2; shift;; -db) db=$2; shift;; esac; shift; done
if [ -n "${STUB_MISMATCH_AT:-}" ] && [ "$to" -ge "$STUB_MISMATCH_AT" ]; then echo "APP HASH MISMATCH at $STUB_MISMATCH_AT"; exit 2; fi
mkdir -p "$db/application.db"; echo "$to" > "$db/application.db/HEIGHT"
echo "done: applied 1 blocks (to height $to), 1 ops in 0.1s; verified against trusted headers: 1, recorded hash only: 0"
EOF
cat > "$S/celestia-appd" <<'EOF'
#!/usr/bin/env bash
# start stub: prints the clean-halt panic at --halt-height, or a consensus failure with STUB_APPD_FAIL=1
while [ $# -gt 0 ]; do case $1 in --halt-height) h=$2; shift;; --home) home=$2; shift;; esac; shift; done
[ -f "$home/data/application.db/HEIGHT" ] && echo "app db at $(cat "$home/data/application.db/HEIGHT")"
echo "env=${STUB_ENV:-unset}"
if [ "${STUB_APPD_FAIL:-0}" = 1 ]; then echo "CONSENSUS FAILURE!!! err=wrong Block.Header.AppHash"; exit 1; fi
echo "panic: halting node per configuration at height $h"; exit 2
EOF
cat > "$S/fsboot" <<'EOF'
#!/usr/bin/env bash
case $1 in
  bootstrap) echo "[fsboot] bootstrapped $*" >> "$STUB_LOG";;
esac
EOF
cat > "$S/fshdr" <<'EOF'
#!/usr/bin/env bash
# headers stub: prints the rows of $STUB_CHAIN (the node's block store) in -from..-to, up to STUB_BS_HEIGHT-1
[ "$1" = headers ] || exit 64
while [ $# -gt 0 ]; do case $1 in -from) lo=$2; shift;; -to) hi=$2; shift;; esac; shift; done
echo "# fshdr headers stub"
awk -F'\t' -v lo="$lo" -v hi="$hi" -v top="${STUB_BS_HEIGHT:-999999999}" '$1>=lo && $1<=hi && $1<top' "$STUB_CHAIN"
EOF
if ! command -v zstd >/dev/null 2>&1; then
  cat > "$S/zstd" <<'EOF'
#!/usr/bin/env bash
# minimal zstd stand-in for hosts without zstd: -o FILE compresses stdin, -dc FILE decompresses
out='' dec=0 in=''
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift;; -dc) dec=1;; -*) ;; *) in=$1;; esac; shift; done
if [ "$dec" = 1 ]; then cat "$in"; else cat > "$out"; fi
EOF
fi
chmod +x "$S"/*
export PATH=$S:$PATH STUB_BUCKET_ROOT=$T/bucket GCLOUD=$S/gcloud STUB_LOG=$T/stub.log
: > "$STUB_LOG"

# a fake verified chain: row h = h, app hash after h, hash of header h+1
EXP=$T/expected.tsv
for h in $(seq 1 120); do printf '%d\t%064X\t%064X\n' "$h" "$h" "$((h + 1000))"; done > "$EXP"
export STUB_CHAIN=$EXP
mkhome() { mkdir -p "$1/config" "$1/data"; echo '{"chain_id": "test-1"}' > "$1/config/genesis.json"; printf 'db_backend = "pebbledb"\npersistent_peers = ""\n\n[blocksync]\nversion = "v0"\n' > "$1/config/config.toml"
  printf 'minimum-gas-prices = ""\n' > "$1/config/app.toml"; }

echo "== apply-checkpoints.sh --dry-run"
out=$("$APPLY" --dry-run --home /nonexistent --changesets /cs --expected /e.tsv --chain-id test-1 --bucket gs://bkt/run1 \
  --every 250000 --to 600000 --manifest /m.json --pub /k.pub --applier /bin/cstool 2>&1)
expect_has "dry: verifies bundle" "$out" "DRY-RUN: manifest verify -dir /cs -manifest /m.json -sig /m.json.sig -pub /k.pub -chain-id test-1 -print-env"
expect_has "dry: applies to 250000" "$out" "DRY-RUN: /bin/cstool apply -db /nonexistent/data -backend goleveldb -changesets /cs -expected /e.tsv -require-trusted -to 250000 -parallel"
expect_has "dry: applies to 500000" "$out" "-require-trusted -to 500000"
expect_lacks "dry: no checkpoint above --to" "$out" "-to 750000"
expect_has "dry: tarball" "$out" "DRY-RUN: tar -C /nonexistent/data -cf - application.db | zstd -q -T0 -3 -o /nonexistent/fastsync-orch/ckpt-250000/application.db.tar.zst"
expect_has "dry: uploads CHECKPOINT" "$out" "gs://bkt/run1/checkpoints/500000/CHECKPOINT"
order=$(grep -n 'storage cp' <<< "$out" | grep 'checkpoints/250000' | sed 's/.*checkpoints\/250000\///')
expect_eq "dry: CHECKPOINT uploaded last" "$order" "$(printf '%s\n' application.db.tar.zst application.db.tar.zst.sha256 CHECKPOINT)"
set +e; "$APPLY" --dry-run --home /h --changesets /cs --expected /e --chain-id test-1 --bucket gs://bkt --to 10 --applier a >/dev/null 2>&1; rc=$?; set -e
expect_rc "dry: refuses without manifest or --skip-bundle-verify" "$rc" 1
set +e; "$APPLY" --dry-run --home /h --changesets /cs --expected /e --chain-id 'bad id' --bucket gs://bkt --to 10 --skip-bundle-verify --applier a >/dev/null 2>&1; rc=$?; set -e
expect_rc "dry: rejects bad chain id" "$rc" 1

echo "== apply-checkpoints.sh with stubs"
H=$T/applyhome; mkhome "$H"
set +e; out=$("$APPLY" --home "$H" --changesets "$T" --expected "$EXP" --chain-id test-1 --bucket gs://bkt/run1 \
  --every 40 --to 100 --skip-bundle-verify --applier cstool 2>&1); rc=$?; set -e
[ "$rc" = 0 ] || echo "$out"
expect_rc "apply: succeeds" "$rc" 0
for c in 40 80; do
  d=$T/bucket/bkt/run1/checkpoints/$c
  check "apply: checkpoint $c published" test -f "$d/CHECKPOINT" -a -f "$d/application.db.tar.zst"
  expect_eq "apply: checkpoint $c app hash" "$(sed -n 's/^app_hash_after=//p' "$d/CHECKPOINT")" "$(printf '%064X' "$c")"
  check "apply: checkpoint $c sha256" sha256sum -c --status <(sed "s#  #  $d/#" "$d/application.db.tar.zst.sha256")
done
out=$("$APPLY" --home "$H" --changesets "$T" --expected "$EXP" --chain-id test-1 --bucket gs://bkt/run1 \
  --every 40 --to 100 --skip-bundle-verify --applier cstool 2>&1)
expect_has "apply: rerun skips published" "$out" "checkpoint 40 already published"
H2=$T/applyhome2; mkhome "$H2"
set +e; STUB_MISMATCH_AT=80 "$APPLY" --home "$H2" --changesets "$T" --expected "$EXP" --chain-id test-1 --bucket gs://bkt/run2 \
  --every 40 --to 100 --skip-bundle-verify --applier cstool >/dev/null 2>&1; rc=$?; set -e
expect_rc "apply: mismatch aborts" "$rc" 1
check "apply: mismatch poisons DB" test -f "$H2/fastsync-orch/POISONED"
check "apply: nothing published after mismatch" test ! -e "$T/bucket/bkt/run2/checkpoints/80"
set +e; "$APPLY" --home "$H2" --changesets "$T" --expected "$EXP" --chain-id test-1 --bucket gs://bkt/run2 \
  --every 40 --to 100 --skip-bundle-verify --applier cstool >/dev/null 2>&1; rc=$?; set -e
expect_rc "apply: refuses to resume a poisoned DB" "$rc" 1
mkhome "$T/wrongchain"; echo '{"chain_id": "other"}' > "$T/wrongchain/config/genesis.json"
set +e; "$APPLY" --home "$T/wrongchain" --changesets "$T" --expected "$EXP" --chain-id test-1 --bucket gs://bkt/run3 \
  --to 40 --every 40 --skip-bundle-verify --applier cstool >/dev/null 2>&1; rc=$?; set -e
expect_rc "apply: genesis chain id mismatch" "$rc" 1

echo "== segment-run.sh --dry-run"
PEER=0123456789abcdef0123456789abcdef01234567@10.0.0.2:26656
out=$("$SEG" --dry-run --bucket gs://bkt/run1 --checkpoint 250000 --end 500000 --home /h --expected /e.tsv --chain-id test-1 \
  --rpc http://rpc:26657 --peer "$PEER" --appd /bin/appd --fsboot /bin/fsboot --fshdr /bin/fshdr --segment-sh /s/segment.sh \
  --max-requesters 200 --fastindex --env "STUB_ENV=1" --start-flags "--log_level warn" 2>&1)
expect_has "dry: downloads checkpoint" "$out" "storage cp gs://bkt/run1/checkpoints/250000/CHECKPOINT gs://bkt/run1/checkpoints/250000/application.db.tar.zst.sha256 gs://bkt/run1/checkpoints/250000/application.db.tar.zst /h/segment/"
expect_has "dry: extracts" "$out" "DRY-RUN: zstd -q -dc /h/segment/application.db.tar.zst | tar -C /h/data -xf - application.db"
expect_has "dry: fastindex" "$out" "DRY-RUN: fastindex -db /h/data -backend goleveldb"
expect_has "dry: fsboot" "$out" "DRY-RUN: /bin/fsboot bootstrap -home /h -height 250000 -expected /e.tsv -rpc http://rpc:26657"
expect_has "dry: inter-block cache" "$out" 'DRY-RUN: set_toml /h/config/app.toml performance inter-block-cache \"sync-only\"'
expect_has "dry: max requesters" "$out" "DRY-RUN: set_toml /h/config/config.toml blocksync max_requesters 200"
expect_has "dry: segment.sh" "$out" "DRY-RUN: env STUB_ENV=1 APPD=/bin/appd FSHDR=/bin/fshdr /s/segment.sh /h 500000 26 $PEER /e.tsv --log_level warn"
expect_has "dry: result path" "$out" "gs://bkt/run1/results/250001-500000/"
out=$("$SEG" --dry-run --bucket gs://bkt --checkpoint genesis --end 30 --home /h --expected /e --chain-id test-1 --peer "$PEER" --inter-block-cache keep 2>&1)
expect_lacks "dry genesis: no download" "$out" "checkpoints/"
expect_lacks "dry genesis: no bootstrap" "$out" "bootstrap -home"
expect_lacks "dry genesis: keep app.toml" "$out" "set_toml"
expect_has "dry genesis: height 0 marker" "$out" "height\\t%s\\n' 0 > /h/CHECKPOINT"
for args in "--checkpoint 100 --end 110" "--checkpoint 0 --end 30" "--checkpoint x --end 30" "--checkpoint 5 --end 30 (no --rpc)" \
  "--checkpoint genesis --end 30 --env BAD" "--checkpoint genesis --end 30 --max-requesters 1" \
  "--checkpoint genesis --end 30 --inter-block-cache on" "--checkpoint genesis --end 30 --peer nope (bad peer)"; do
  set +e; read -r -a a <<< "${args% (*}"
  "$SEG" --dry-run --bucket gs://bkt --home /h --expected /e --chain-id test-1 --peer "$PEER" "${a[@]}" >/dev/null 2>&1; rc=$?; set -e
  check "segment rejects: $args" [ "$rc" = 1 ]
done

echo "== segment-run.sh with stubs"
seg() { # $1 = name; rest = extra args. Fresh home each time; segment 41..80 from checkpoint 40 (published above)
  local h=$T/seg-$1; shift; rm -rf "$h"; mkhome "$h"
  set +e; "$SEG" --bucket gs://bkt/run1 --checkpoint 40 --end 80 --home "$h" --expected "$EXP" --chain-id test-1 \
    --rpc http://rpc --peer "$PEER" --segment-sh "$HERE/../segment.sh" "$@" > "$T/seg.out" 2>&1; local rc=$?; set -e
  echo "$rc"
}
res() { sed -n "s/^$1=//p" "$T/bucket/bkt/run1/results/41-80/RESULT"; }
expect_rc "segment: pass" "$(seg pass --env STUB_ENV=1 --max-requesters 300)" 0
expect_eq "segment: PASS uploaded" "$(res status)" PASS
check "segment: --env reaches appd" grep -q "env=1" "$T/bucket/bkt/run1/results/41-80/segment.log.tail"
check "segment: inter-block cache set" grep -qx 'inter-block-cache = "sync-only"' "$T/seg-pass/config/app.toml"
# shellcheck disable=SC2016 # awk program
check "segment: max_requesters in [blocksync]" awk '/^\[blocksync\]/ {s = 1; next} /^\[/ {s = 0} s && $0 == "max_requesters = 300" {f = 1} END {exit !f}' "$T/seg-pass/config/config.toml"
check "segment: peer configured" grep -q "persistent_peers = \"$PEER\"" "$T/seg-pass/config/config.toml"
expect_has "segment: OK reason" "$(res reason)" "SEGMENT 40..80: OK"
check "segment: checkpoint DB extracted" grep -q "app db at 40" "$T/bucket/bkt/run1/results/41-80/segment.log.tail"
check "segment: fsboot at checkpoint" grep -q "bootstrap -home $T/seg-pass -height 40" "$STUB_LOG"
expect_rc "segment: consensus failure" "$(STUB_APPD_FAIL=1 seg cf)" 2
expect_eq "segment: FAIL uploaded" "$(res status)" FAIL
expect_has "segment: failure reason" "$(res reason)" "FAILED"
expect_rc "segment: short blockstore" "$(STUB_BS_HEIGHT=80 seg bs)" 2
sed '75s/^\(75\t\)./\1F/' "$EXP" > "$T/forked.tsv"
expect_rc "segment: boundary mismatch" "$(STUB_CHAIN=$T/forked.tsv seg fork)" 2
expect_has "segment: boundary reason" "$(res reason)" "BOUNDARY MISMATCH"
cp "$T/bucket/bkt/run1/checkpoints/40/application.db.tar.zst" "$T/tb.bak"
printf 'X' >> "$T/bucket/bkt/run1/checkpoints/40/application.db.tar.zst"
expect_rc "segment: tampered checkpoint tarball" "$(seg tamper)" 2
expect_has "segment: tamper reason" "$(res reason)" "sha256 mismatch"
cp "$T/tb.bak" "$T/bucket/bkt/run1/checkpoints/40/application.db.tar.zst"
sed -i 's/^app_hash_after=.*/app_hash_after=00/' "$T/bucket/bkt/run1/checkpoints/40/CHECKPOINT"
expect_rc "segment: checkpoint app hash != verified chain" "$(seg apphash)" 2
expect_has "segment: app hash reason" "$(res reason)" "!= verified"

echo
echo "passed: $PASS  failed: $FAILS"
[ "$FAILS" = 0 ]
