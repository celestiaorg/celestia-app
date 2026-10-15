#!/bin/sh

# Generate a proposal from `celestia-appd query gov params --output json`.
# This script only writes JSON to stdout; it does not submit a transaction.
set -eu

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 gov-params.json" >&2
  exit 1
fi

jq -e '
  .params as $params
  | [
      "min_deposit", "max_deposit_period", "voting_period", "quorum",
      "threshold", "veto_threshold", "min_initial_deposit_ratio",
      "proposal_cancel_ratio", "proposal_cancel_dest", "expedited_voting_period",
      "expedited_threshold", "expedited_min_deposit", "burn_vote_quorum",
      "burn_proposal_deposit_prevote", "burn_vote_veto", "min_deposit_ratio"
    ] as $required
  | if ($params | type) != "object" then
      error("expected a gov params query response containing .params")
    elif ($required - ($params | keys) | length) != 0 then
      error("expected complete governance params; MsgUpdateParams replaces all params")
    elif ($params.min_deposit | type) != "array" or ($params.min_deposit | length) == 0 then
      error("expected a nonempty min_deposit")
    else
      {
        messages: [{
          "@type": "/cosmos.gov.v1.MsgUpdateParams",
          authority: "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
          params: ($params + {proposal_cancel_ratio: "0.050000000000000000"})
        }],
        metadata: "",
        deposit: ($params.min_deposit | map(.amount + .denom) | join(",")),
        title: "Reduce the proposal cancellation penalty to 5%",
        summary: "Set proposal_cancel_ratio to 0.05 so voluntary proposal cancellation returns 95% of each deposit while retaining a 5% cancellation charge during both deposit and voting periods. Preserve all other governance parameters, including the quorum, prevote and veto burn settings.",
        expedited: false
      }
    end
' "$1"
