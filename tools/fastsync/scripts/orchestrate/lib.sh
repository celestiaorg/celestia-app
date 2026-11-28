#!/usr/bin/env bash
# lib.sh: helpers for the orchestrate/*.sh scripts on top of ../lib-apply.sh. Source it; do not run it.
#
# Bucket layout:
#   <prefix>/checkpoints/<h>/application.db.tar.zst   app DB after block h (zstd tarball of application.db)
#   <prefix>/checkpoints/<h>/CHECKPOINT               key=value metadata; uploaded LAST = checkpoint complete
#   <prefix>/results/<start>-<end>/RESULT             key=value segment result (status=PASS|FAIL)
#   <prefix>/results/<start>-<end>/segment.log.tail   last lines of the segment node log
# GCLOUD overrides the gcloud command, e.g. GCLOUD="env -u CLOUDSDK_AUTH_ACCESS_TOKEN gcloud".

# shellcheck source-path=SCRIPTDIR source=../lib-apply.sh
. "$(dirname "${BASH_SOURCE[0]}")/../lib-apply.sh"
DRY_RUN=${DRY_RUN:-0}
read -r -a GCLOUD_CMD <<< "${GCLOUD:-gcloud}"

# run_sh STRING: run a pipeline, or print it with --dry-run.
run_sh() {
  if [ "$DRY_RUN" = 1 ]; then echo "+ $1"; else bash -o pipefail -c "$1"; fi
}
need_uint() { is_uint "$2" || die "$1 must be a non-negative integer, got '$2'"; }
need_tools() {
  local t
  for t in "$@"; do command -v "$t" >/dev/null 2>&1 || die "missing tool: $t"; done
}
# check_bucket URL: a gs:// prefix without spaces, quotes, globs or '..' (it ends up in commands).
check_bucket() {
  [[ $1 =~ ^gs://[a-z0-9][a-z0-9._-]{1,220}(/[A-Za-z0-9._/-]*)?$ && $1 != *..* ]] || die "bucket must look like gs://bucket[/prefix], got '$1'"
}
check_chain_id() { [[ $1 =~ ^[A-Za-z0-9._-]{1,64}$ ]] || die "bad chain id '$1'"; }
gcs() { run "${GCLOUD_CMD[@]}" storage "$@"; }
# gcs_exists URL: true if the object exists (always false in dry runs, so they show the whole plan).
gcs_exists() {
  [ "$DRY_RUN" = 1 ] && return 1
  "${GCLOUD_CMD[@]}" storage ls "$1" >/dev/null 2>&1
}
# check_expected FILE CHAIN_ID: FILE must be fshdr output verified for CHAIN_ID (as segment.sh requires).
check_expected() {
  local first
  first=$(head -1 "$1")
  case $first in
    "# fshdr verify chain=$2 "* | "# fshdr forward chain=$2 "*) ;;
    *) die "$1 is not a verified fshdr file for chain $2 (first line: $first)" ;;
  esac
}
# expected_col H COL FILE: column COL of row H of an expected.tsv (2 = app hash after H). Empty if absent.
expected_col() { awk -F'\t' -v h="$1" -v c="$2" '$1==h {print $c; exit}' "$3"; }
# set_toml FILE SECTION KEY VALUE: set KEY = VALUE (written as is; quote strings) in [SECTION].
set_toml() {
  local f=$1 sec=$2 key=$3 val=$4 tmp
  tmp=$(mktemp "$f.XXXXXX")
  awk -v sec="[$sec]" -v key="$key" -v val="$val" '
    function emit() { if (insec && !done) { print key " = " val; done = 1 } }
    /^\[/ { emit(); insec = ($0 == sec); print; next }
    insec && $0 ~ "^" key " *=" { if (!done) print key " = " val; done = 1; next }
    { print }
    END { emit(); if (!done) { print ""; print sec; print key " = " val } }' "$f" > "$tmp"
  mv "$tmp" "$f"
}
