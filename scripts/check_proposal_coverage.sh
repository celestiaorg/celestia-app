#!/usr/bin/env bash
set -euo pipefail

# Check the actual handler functions, not just helpers in the same files.
# go tool cover fails if the profile is malformed or source cannot be resolved.
go tool cover -func="${1:-coverage.txt}" | awk '
    /\/app\/prepare_proposal.go:[0-9]+:[[:space:]]/ && $2 == "PrepareProposalHandler" {
        print; prepare = ($3 + 0 > 0)
    }
    /\/app\/process_proposal.go:[0-9]+:[[:space:]]/ && $2 == "ProcessProposalHandler" {
        print; process = ($3 + 0 > 0)
    }
    /^total:/ { print }
    END {
        if (!prepare || !process) {
            print "Both proposal handlers must have executed statements" > "/dev/stderr"
            exit 1
        }
    }
'
