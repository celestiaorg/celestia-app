#!/usr/bin/env bash
# plan-segments.sh: print the re-execution segment plan for a height range and a number of segment VMs.
#
#   plan-segments.sh --to H [--from 1] [--every 250000] [--vms 8] [--format tsv|checkpoints|commands]
#                    [--bucket gs://B/prefix] [--run-args "..."] [--script PATH]
#
# Checkpoints are the app DB after block c, for c = from-1 (none when from = 1: segment 0 starts from genesis)
# and every multiple of --every in (from-1, H). Segment k starts from checkpoint c_k and fully re-executes
# c_k+1 .. c_{k+1} (the last segment ends at H). Segments are assigned to VMs round-robin; "wave" is the
# round in which a VM runs it (VM v runs its segments one after another).
#
# Formats:
#   tsv          (default) one line per segment: seg start_ckpt first_height end_height blocks vm wave
#   checkpoints  the comma-separated checkpoint heights, for apply-checkpoints.sh --checkpoints
#   commands     one segment-run.sh command line per segment, prefixed by "vm<N><TAB>"; --run-args is
#                appended verbatim (the flags common to every segment: --home, --expected, --rpc, ...);
#                --script is the path of segment-run.sh on the segment VMs (default: next to this script)
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
LOG_TAG=plan-segments
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$HERE/lib.sh"

FROM=1 TO='' EVERY=250000 VMS=1 FORMAT=tsv BUCKET='' RUN_ARGS='' SCRIPT=$HERE/segment-run.sh
usage() { sed -n 2,4p "$0" >&2; exit 2; }
while [ $# -gt 0 ]; do
  case $1 in
    --from) FROM=$2; shift;; --to) TO=$2; shift;; --every) EVERY=$2; shift;; --vms) VMS=$2; shift;;
    --format) FORMAT=$2; shift;; --bucket) BUCKET=$2; shift;; --run-args) RUN_ARGS=$2; shift;;
    --script) SCRIPT=$2; shift;;
    -h|--help) usage;;
    *) echo "unknown arg $1" >&2; usage;;
  esac
  shift
done
[ -n "$TO" ] || usage
need_uint --from "$FROM"; need_uint --to "$TO"; need_uint --every "$EVERY"; need_uint --vms "$VMS"
[ "$FROM" -ge 1 ] || die "--from must be >= 1"
[ "$TO" -ge "$FROM" ] || die "--to ($TO) must be >= --from ($FROM)"
[ "$EVERY" -ge 1 ] || die "--every must be >= 1"
[ "$VMS" -ge 1 ] || die "--vms must be >= 1"
case $FORMAT in tsv|checkpoints|commands) ;; *) die "--format must be tsv, checkpoints or commands";; esac
if [ "$FORMAT" = commands ]; then
  [ -n "$BUCKET" ] || die "--format commands needs --bucket"
  check_bucket "$BUCKET"
fi

# checkpoint heights: from-1 (0 = genesis), then multiples of EVERY in (from-1, TO)
first_mult=$(( ((FROM - 1) / EVERY + 1) * EVERY ))
NSEG=$(( 1 + (first_mult < TO ? (TO - 1 - first_mult) / EVERY + 1 : 0) ))
[ "$NSEG" -le 100000 ] || die "$NSEG segments: raise --every"
starts=("$((FROM - 1))")
for ((c = first_mult; c < TO; c += EVERY)); do starts+=("$c"); done

if [ "$FORMAT" = checkpoints ]; then
  out=()
  for c in "${starts[@]}"; do [ "$c" -gt 0 ] && out+=("$c"); done
  (IFS=,; echo "${out[*]}")
  exit 0
fi

WAVES=$(( (NSEG + VMS - 1) / VMS ))
if [ "$FORMAT" = tsv ]; then
  echo "# plan: heights $FROM..$TO, checkpoint every $EVERY, $VMS VM(s) -> $NSEG segment(s), $WAVES wave(s)"
  printf 'seg\tstart_ckpt\tfirst_height\tend_height\tblocks\tvm\twave\n'
fi
for ((k = 0; k < NSEG; k++)); do
  s=${starts[k]}
  if [ $((k + 1)) -lt "$NSEG" ]; then e=${starts[k + 1]}; else e=$TO; fi
  ck=$s; [ "$s" = 0 ] && ck=genesis
  vm=$((k % VMS)) wave=$((k / VMS))
  if [ "$FORMAT" = tsv ]; then
    printf '%d\t%s\t%d\t%d\t%d\t%d\t%d\n' "$k" "$ck" "$((s + 1))" "$e" "$((e - s))" "$vm" "$wave"
  else
    printf 'vm%d\t%q --bucket %q --checkpoint %s --end %d%s\n' "$vm" "$SCRIPT" "$BUCKET" "$ck" "$e" "${RUN_ARGS:+ $RUN_ARGS}"
  fi
done
