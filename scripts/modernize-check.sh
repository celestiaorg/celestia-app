#!/bin/bash

# Modernize check script for celestia-app CI
# This script checks for modernize issues without applying fixes
# Fails if any modernize issues are found

set -e

cd "$(dirname "$0")/.."

echo "Running Go modernize check..."

# Run modernize without -fix flag to only check for issues
# The tool will exit with non-zero code if issues are found
go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -test ./...
