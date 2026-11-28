#!/usr/bin/env bash
# plan-segments.sh: print the re-execution segment plan for a height range and a number of segment VMs.
#
#   plan-segments.sh --to H [--from 1] [--every 250000] [--vms 1]
#
# Checkpoints are the app DB after block c for every multiple c of --every below H, as apply-checkpoints.sh
# publishes them; from-1 must be one of them (or 0: segment 0 then starts from genesis). Segment k starts
# from checkpoint c_k and fully re-executes c_k+1 .. c_{k+1} (the last segment ends at H). Segments go to VMs
# round-robin; "wave" is the round in which a VM runs it.
set -euo pipefail
LOG_TAG=plan-segments
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$(dirname "$0")/lib.sh"

FROM=1 TO='' EVERY=250000 VMS=1
usage() { sed -n 2,4p "$0" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case $1 in
    --from) FROM=$2; shift ;;
    --to) TO=$2; shift ;;
    --every) EVERY=$2; shift ;;
    --vms) VMS=$2; shift ;;
    -h | --help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
  shift
done
[ -n "$TO" ] || usage
need_uint --from "$FROM"; need_uint --to "$TO"; need_uint --every "$EVERY"; need_uint --vms "$VMS"
[ "$FROM" -ge 1 ] && [ "$TO" -ge "$FROM" ] || die "need 1 <= --from <= --to"
[ "$EVERY" -ge 1 ] && [ "$VMS" -ge 1 ] || die "--every and --vms must be >= 1"
[ $(((FROM - 1) % EVERY)) = 0 ] || die "--from - 1 must be a checkpoint height (a multiple of --every)"

first_mult=$((FROM - 1 + EVERY))
NSEG=$(( 1 + (first_mult < TO ? (TO - 1 - first_mult) / EVERY + 1 : 0) ))
[ "$NSEG" -le 100000 ] || die "$NSEG segments: raise --every"
starts=("$((FROM - 1))")
for ((c = first_mult; c < TO; c += EVERY)); do starts+=("$c"); done

echo "# plan: heights $FROM..$TO, checkpoint every $EVERY, $VMS VM(s) -> $NSEG segment(s), $(( (NSEG + VMS - 1) / VMS )) wave(s)"
printf 'seg\tstart_ckpt\tfirst_height\tend_height\tblocks\tvm\twave\n'
for ((k = 0; k < NSEG; k++)); do
  s=${starts[k]}
  if [ $((k + 1)) -lt "$NSEG" ]; then e=${starts[k + 1]}; else e=$TO; fi
  ck=$s; [ "$s" = 0 ] && ck=genesis
  printf '%d\t%s\t%d\t%d\t%d\t%d\t%d\n' "$k" "$ck" "$((s + 1))" "$e" "$((e - s))" "$((k % VMS))" "$((k / VMS))"
done
