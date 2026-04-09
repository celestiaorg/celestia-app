#!/usr/bin/env bash
set -euo pipefail

SWAGGER_TMP="tmp-swagger-gen"
OUTPUT_DIR="docs/swagger"
OUTPUT="$OUTPUT_DIR/swagger.json"

rm -rf "$SWAGGER_TMP"
mkdir -p "$SWAGGER_TMP" "$OUTPUT_DIR"

cd proto

# Generate OpenAPI specs from Celestia protos
buf generate --template buf.gen.openapiv2.yaml

# Generate OpenAPI specs from Cosmos SDK dependency protos
buf generate buf.build/cosmos/cosmos-sdk \
  --template buf.gen.openapiv2.yaml \
  --path cosmos/auth/v1beta1 \
  --path cosmos/bank/v1beta1 \
  --path cosmos/staking/v1beta1 \
  --path cosmos/gov/v1 \
  --path cosmos/gov/v1beta1 \
  --path cosmos/distribution/v1beta1 \
  --path cosmos/slashing/v1beta1 \
  --path cosmos/feegrant/v1beta1 \
  --path cosmos/authz/v1beta1 \
  --path cosmos/evidence/v1beta1 \
  --path cosmos/upgrade/v1beta1 \
  --path cosmos/params/v1beta1 \
  --path cosmos/consensus/v1 \
  --path cosmos/tx/v1beta1

cd ..

# Merge all generated swagger files into one
jq -s 'reduce .[] as $item (
  {
    "swagger": "2.0",
    "info": {
      "title": "Celestia App - REST API",
      "description": "Auto-generated OpenAPI spec for celestia-app gRPC-gateway endpoints.",
      "version": "v9"
    },
    "consumes": ["application/json"],
    "produces": ["application/json"],
    "paths": {},
    "definitions": {}
  };
  .paths += ($item.paths // {}) |
  .definitions += ($item.definitions // {})
)' $(find "$SWAGGER_TMP" -name '*.swagger.json' | sort) > "$OUTPUT"

rm -rf "$SWAGGER_TMP"

echo "Generated $OUTPUT with $(jq '.paths | length' "$OUTPUT") endpoints"
