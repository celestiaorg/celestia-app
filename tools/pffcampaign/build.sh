#!/usr/bin/env bash
set -euo pipefail
export GOAMD64=v1
mkdir -p bin logs
for arm in main pr-stack complete; do
  for cgo in 0 1; do
    for limits in production override; do
      tags=pffcampaign
      if [ "$limits" = override ]; then tags=pffcampaign,benchmarks; fi
      name="$arm-cgo$cgo-$limits"
      (cd "source/$arm" && CGO_ENABLED="$cgo" go test -tags="$tags" -c ./app/test -o "../../bin/$name.test") > "logs/build-$name.log" 2>&1
      sha256sum "bin/$name.test" >> binary-sha256.txt
      go version -m "bin/$name.test" > "logs/binary-$name.txt"
    done
    (cd "source/$arm" && CGO_ENABLED="$cgo" go test -tags=pffcampaign -c ./x/fibre/keeper -o "../../bin/$arm-cgo$cgo-cert.test") > "logs/build-$arm-cgo$cgo-cert.log" 2>&1
    sha256sum "bin/$arm-cgo$cgo-cert.test" >> binary-sha256.txt
  done
 done
