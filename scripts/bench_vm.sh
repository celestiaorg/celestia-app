#!/usr/bin/env bash
# Provision and drive the GCE machines the PayForFibre ABCI benchmarks run on.
#
#   ./scripts/bench_vm.sh create [shape ...]   create the VMs (default: all)
#   ./scripts/bench_vm.sh status               list them and whether they are ready
#   ./scripts/bench_vm.sh sync <shape>         copy this tree to one VM
#   ./scripts/bench_vm.sh run <shape> [cores]  run the benchmark sweep on one VM
#   ./scripts/bench_vm.sh destroy              delete every VM this script made
#
# These bill from creation. Destroy them as soon as the run is collected.
set -euo pipefail

ZONE=us-central1-a
PROJECT=fibreda
PREFIX=pff-bench
# 16 is the control: it is GCDefaultValidatorMachineType and the shape behind
# every prior fleet run. 60 is ~4x it, single socket. The family ceiling (360)
# is deliberately out: at ~$13/hr it costs more than the rest combined, and the
# taskset sweep on the 60 already gives the per-core curve.
SHAPES=(16 60)
GO_VERSION=1.26.6
REPO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RESULTS_DIR=${RESULTS_DIR:-$REPO_DIR/bench-results}

gc() { gcloud --project="$PROJECT" "$@"; }
name_of() { echo "$PREFIX-$1"; }

startup_script() {
  cat <<EOF
#!/bin/bash
set -eux
apt-get update
apt-get install -y build-essential git rsync
curl -fsSLo /tmp/go.tar.gz https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=\$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
chmod 644 /etc/profile.d/go.sh
touch /var/lib/bench-vm-ready
EOF
}

cmd_create() {
  local shapes=("${@:-${SHAPES[@]}}")
  [ $# -gt 0 ] && shapes=("$@")
  local script
  script=$(mktemp)
  startup_script > "$script"
  for shape in "${shapes[@]}"; do
    echo "creating $(name_of "$shape") (c3d-highcpu-$shape)"
    gc compute instances create "$(name_of "$shape")" \
      --zone="$ZONE" \
      --machine-type="c3d-highcpu-$shape" \
      --image-family=ubuntu-2404-lts-amd64 \
      --image-project=ubuntu-os-cloud \
      --boot-disk-type=hyperdisk-balanced \
      --boot-disk-size=200GB \
      --labels=purpose=pff-abci-bench \
      --metadata-from-file=startup-script="$script" &
  done
  wait
  rm -f "$script"
  echo "created. They bill from now; run '$0 destroy' when finished."
}

cmd_status() {
  gc compute instances list --filter="labels.purpose=pff-abci-bench" \
    --format="table(name,machineType.basename(),status,networkInterfaces[0].accessConfigs[0].natIP)"
  for shape in "${SHAPES[@]}"; do
    local n
    n=$(name_of "$shape")
    if gc compute ssh "$n" --zone="$ZONE" --quiet --command="test -f /var/lib/bench-vm-ready" >/dev/null 2>&1; then
      echo "$n: toolchain ready"
    else
      echo "$n: not ready (or unreachable)"
    fi
  done
}

cmd_sync() {
  local n
  n=$(name_of "$1")
  tar -C "$REPO_DIR" --exclude=.git --exclude=build --exclude=bench-results --exclude='*.test' -czf /tmp/pff-repo.tgz .
  gc compute scp /tmp/pff-repo.tgz "$n:~/pff-repo.tgz" --zone="$ZONE" --quiet
  gc compute ssh "$n" --zone="$ZONE" --quiet --command="
    set -eu
    rm -rf ~/celestia-app && mkdir -p ~/celestia-app
    tar -C ~/celestia-app -xzf ~/pff-repo.tgz && rm ~/pff-repo.tgz
    export PATH=\$PATH:/usr/local/go/bin
    cd ~/celestia-app && go mod download
  "
  rm -f /tmp/pff-repo.tgz
  echo "synced to $n"
}

cmd_run() {
  local shape=$1 cores=${2:-}
  local n
  n=$(name_of "$shape")
  mkdir -p "$RESULTS_DIR"
  local taskset_prefix=""
  local label="all"
  if [ -n "$cores" ]; then
    taskset_prefix="taskset -c 0-$((cores - 1))"
    label="$cores"
  fi
  # taskset, not -cpu: the parallel verifier sizes itself from runtime.NumCPU,
  # which GOMAXPROCS does not change.
  gc compute ssh "$n" --zone="$ZONE" --quiet --command="
    export PATH=\$PATH:/usr/local/go/bin
    cd ~/celestia-app
    $taskset_prefix go test -tags benchmarks -run '^\$' -bench '_PFF' -benchtime=5x -count=10 -timeout 180m ./app/benchmarks/
  " | tee "$RESULTS_DIR/c3d-highcpu-$shape.cores-$label.txt"
  echo "wrote $RESULTS_DIR/c3d-highcpu-$shape.cores-$label.txt"
}

cmd_destroy() {
  local names
  names=$(gc compute instances list --filter="labels.purpose=pff-abci-bench" --format="value(name)")
  if [ -z "$names" ]; then
    echo "nothing to destroy"
    return
  fi
  # shellcheck disable=SC2086
  gc compute instances delete $names --zone="$ZONE" --quiet
  echo "destroyed: $names"
}

case "${1:-}" in
  create)  shift; cmd_create "$@" ;;
  status)  cmd_status ;;
  sync)    shift; cmd_sync "$@" ;;
  run)     shift; cmd_run "$@" ;;
  destroy) cmd_destroy ;;
  *) sed -n '2,12p' "$0"; exit 1 ;;
esac
