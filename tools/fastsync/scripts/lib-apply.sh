#!/usr/bin/env bash
# lib-apply.sh: helpers shared by fastsync.sh and orchestrate/*.sh. Source it; do not run it.
#
# Logging (LOG_TAG, default "fastsync"), dry run (DRY_RUN=1 prints "+ cmd" instead of running it) and the
# verified apply loop. apply_to uses these variables, set by the caller:
#   HOME_DIR (application.db is in $HOME_DIR/data), BACKEND, CS_DIR, EXPECTED (fshdr expected.tsv), WORK,
#   APPLIER, APPLIER50, PARALLEL (1 = -parallel), DRY_RUN
# and maintains CUR (the applier in use; APPLIER50 after a HANDOVER) and APPLIED (the resume marker,
# $WORK/applied-to, written only after a verified apply). Call apply_init first.
#
# Applier interface: `<applier> apply -db DIR -backend B -changesets DIR -expected FILE -require-trusted -to H
# [-parallel]`; exit 2 means an app hash mismatch; output lines "HANDOVER ..." and
# "done ... to height N ... recorded hash only: 0".

log() { echo "[${LOG_TAG:-fastsync} $(date -u +%H:%M:%S)] $*"; }
die() { echo "[${LOG_TAG:-fastsync}] ABORT: $*" >&2; exit 1; }
# run CMD...: execute, or print it with --dry-run.
run() {
  if [ "${DRY_RUN:-0}" = 1 ]; then echo "+ $*"; else "$@"; fi
}
is_uint() { [[ $1 =~ ^[0-9]+$ ]]; }

# shellcheck disable=SC2153 # APPLIER and the other inputs are set by the caller
apply_init() {
  CUR=$APPLIER
  APPLIED=$(cat "$WORK/applied-to" 2>/dev/null || echo 0)
  is_uint "$APPLIED" || die "bad resume marker $WORK/applied-to"
}

apply_to() { # $1 = target: applies (APPLIED, target], every height checked against $EXPECTED
  local tgt=$1 rc done_line args
  args=(apply -db "$HOME_DIR/data" -backend "$BACKEND" -changesets "$CS_DIR" -expected "$EXPECTED" -require-trusted -to "$tgt")
  [ "$PARALLEL" = 1 ] && args+=(-parallel)
  [ "$tgt" -le "$APPLIED" ] && return 0
  if [ "${DRY_RUN:-0}" = 1 ]; then
    run "$CUR" "${args[@]}"
    APPLIED=$tgt
    return 0
  fi
  while true; do
    log "apply to $tgt: $CUR ${args[*]}"
    set +e
    "$CUR" "${args[@]}" > "$WORK/apply-$tgt.log" 2>&1
    rc=$?
    set -e
    grep -v '^height .*applied=' "$WORK/apply-$tgt.log" | sed 's/^/    /' || true
    if [ "$rc" = 2 ]; then # the applier committed the bad height before comparing: never resume this DB
      echo "APP HASH MISMATCH while applying to $tgt with $CUR" > "$WORK/POISONED"
      die "APP HASH MISMATCH during apply (see $WORK/apply-$tgt.log); nothing bootstrapped; DB marked POISONED"
    fi
    [ "$rc" = 0 ] || die "$CUR apply failed (rc=$rc)"
    if grep -q '^HANDOVER' "$WORK/apply-$tgt.log" && [ "$CUR" = "$APPLIER" ]; then
      [ -n "$APPLIER50" ] || die "the changesets continue with app v4+ blocks: rerun with --applier50 PATH"
      CUR=$APPLIER50
      log "handover to the v0.50 applier ($CUR)"
      continue
    fi
    break
  done
  done_line=$(grep '^done' "$WORK/apply-$tgt.log" | tail -1)
  [ "$(sed -n 's/.*to height \([0-9]*\).*/\1/p' <<< "$done_line")" = "$tgt" ] || die "applier stopped at '$done_line', expected $tgt"
  grep -q 'recorded hash only: 0' <<< "$done_line" || die "some heights were not verified against trusted headers"
  APPLIED=$tgt
  echo "$tgt" > "$WORK/applied-to"
}
