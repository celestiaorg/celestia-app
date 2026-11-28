#!/usr/bin/env bash
# segment.sh: run and check one full re-execution segment of the hybrid.
#
#   segment.sh [--dry-run] CKPT_HOME END_HEIGHT PORT_PREFIX PEER EXPECTED_TSV [start flags...]
#
# Starts a stock node from a fastsync checkpoint home (application.db and CometBFT stores at c), lets it
# block-sync and fully execute c+1..END+1 (--halt-height END+1), then checks the boundary: the segment's own
# block store must hold the verified headers for END-19..END (height, app hash after h, hash of header h+1).
# A node accepts block END+1 only after checking its AppHash against its own state after END, so a clean
# halt at END+1 with matching headers means the segment re-executed c+1..END and reproduced the chain.
#
# PORT_PREFIX (e.g. 31) gives the segment its own ports (<prefix>656, <prefix>657, ...), so segments and the
# main node can run side by side. PEER is a persistent peer (id@host:port) that serves the blocks.
# Env: APPD (default celestia-appd), FSHDR (default fshdr).
set -euo pipefail

DRY_RUN=0
if [ "${1:-}" = --dry-run ]; then DRY_RUN=1; shift; fi
[ $# -ge 5 ] || { sed -n '2,13p' "$0"; exit 1; }
CK=$1 END=$2 P=$3 PEER=$4 EXP=$5
shift 5
APPD=${APPD:-celestia-appd} FSHDR=${FSHDR:-fshdr}
[[ $END =~ ^[0-9]+$ && $P =~ ^[0-9]+$ ]] || { echo "END_HEIGHT and PORT_PREFIX must be numbers" >&2; exit 1; }
[ "$END" -ge 20 ] || { echo "END_HEIGHT must be >= 20" >&2; exit 1; }
C=$CK/config/config.toml
[ -f "$C" ] && [ -f "$CK/CHECKPOINT" ] && [ -f "$EXP" ] || { echo "need $C, $CK/CHECKPOINT and $EXP" >&2; exit 1; }
# EXPECTED_TSV must be fshdr output verified for this chain, not an unverified dump
CHAIN_ID=$(grep -o '"chain_id": *"[^"]*"' "$CK/config/genesis.json" 2>/dev/null | head -1 | sed 's/.*"\([^"]*\)"$/\1/')
[ -n "$CHAIN_ID" ] || { echo "no chain_id in $CK/config/genesis.json" >&2; exit 1; }
FIRST=$(head -1 "$EXP")
case $FIRST in
  "# fshdr verify chain=$CHAIN_ID "* | "# fshdr forward chain=$CHAIN_ID "*) ;;
  *) echo "$EXP is not a verified fshdr file for chain $CHAIN_ID (first line: $FIRST)" >&2; exit 1 ;;
esac
START=$(awk -F'\t' '$1=="height"{print $2}' "$CK/CHECKPOINT")
[ -n "$START" ] && [ "$START" -lt "$END" ] || { echo "checkpoint height '$START' must be below END $END" >&2; exit 1; }
BACKEND=$(sed -n 's/^db_backend *= *"\(.*\)"/\1/p' "$C" | head -1)
LO=$((END - 19))

if [ "$DRY_RUN" = 1 ]; then
  echo "+ configure $C: ports ${P}656/${P}657/${P}658, persistent_peers=$PEER"
  echo "+ $APPD start --home $CK --halt-height $((END + 1)) $*"
  echo "+ $FSHDR headers -blockstore $CK/data -backend ${BACKEND:-goleveldb} -from $LO -to $END"
  exit 0
fi

sed -i \
  -e "s#^laddr = \"tcp://[0-9.]*:[0-9]*7\"#laddr = \"tcp://127.0.0.1:${P}657\"#" \
  -e "s#^laddr = \"tcp://[0-9.]*:[0-9]*6\"#laddr = \"tcp://127.0.0.1:${P}656\"#" \
  -e "s#^proxy_app = .*#proxy_app = \"tcp://127.0.0.1:${P}658\"#" \
  -e "s#^persistent_peers = .*#persistent_peers = \"$PEER\"#" \
  -e "s#^pprof_laddr = .*#pprof_laddr = \"\"#" \
  -e "s#^grpc_laddr = .*#grpc_laddr = \"tcp://127.0.0.1:${P}098\"#" \
  -e "s#^priv_validator_grpc_laddr = .*#priv_validator_grpc_laddr = \"127.0.0.1:${P}669\"#" \
  -e "s#^prometheus_listen_addr = .*#prometheus_listen_addr = \":${P}660\"#" \
  "$C"

T0=$(date +%s)
set +e
"$APPD" start --home "$CK" --halt-height $((END + 1)) "$@" > "$CK/segment.log" 2>&1
rc=$?
set -e
T1=$(date +%s)
# A multiplexer can leave its embedded child running after the halt; the orphan would keep the ABCI port and
# the application DB, and a later node on the same ports would talk to the wrong app. Stop it.
CKABS=$(cd "$CK" && pwd)
if pkill -f "^$CKABS/bin/"; then
  sleep 2
  pkill -9 -f "^$CKABS/bin/" || true
fi
# The halt at END+1 happens inside FinalizeBlock (a panic "halting node per configuration at height END+1"),
# after block END+1 was validated against the node's own state after END. Any other panic is a failure.
CLEAN_HALT="halting node per configuration at height $((END + 1))"
if grep -q "CONSENSUS FAILURE\|wrong Block.Header.AppHash" "$CK/segment.log" ||
  grep "panic" "$CK/segment.log" | grep -vq "$CLEAN_HALT"; then
  echo "SEGMENT $START..$END: FAILED (rc=$rc, see $CK/segment.log)"
  exit 2
fi
"$FSHDR" headers -blockstore "$CK/data" -backend "${BACKEND:-goleveldb}" -from "$LO" -to "$END" | grep -v '^#' | cut -f1-3 > "$CK/boundary.tsv"
awk -F'\t' -v lo="$LO" -v hi="$END" '$1>=lo && $1<=hi {print $1"\t"$2"\t"$3}' "$EXP" > "$CK/boundary.expected"
if [ "$(wc -l < "$CK/boundary.tsv")" = 20 ] && cmp -s "$CK/boundary.tsv" "$CK/boundary.expected"; then
  echo "SEGMENT $START..$END: OK (re-executed $((END - START)) blocks in $((T1 - T0))s, rc=$rc; boundary $LO..$END matches the verified chain)"
else
  echo "SEGMENT $START..$END: BOUNDARY MISMATCH"
  diff "$CK/boundary.tsv" "$CK/boundary.expected" | head || true
  exit 2
fi
