#!/usr/bin/env bash

set -e

cd proto
proto_dirs=$(find . -name '*.proto' -print0 | xargs -0 -n1 dirname | sort | uniq)
for dir in $proto_dirs; do
  for file in $(find "${dir}" -maxdepth 1 -name '*.proto'); do
      echo "Generating gogo proto code for ${file}"
      buf generate --template buf.gen.gogo.yaml $file
  done
done

cd ..

# move proto files to the right places
cp -r github.com/celestiaorg/celestia-app/* ./
# The x/fibre protos set go_package to a
# separate module path (celestia-app-fibre). Will be fixed in: https://github.com/celestiaorg/celestia-app/issues/7500
cp -r github.com/celestiaorg/celestia-app-fibre/* ./
rm -rf github.com
