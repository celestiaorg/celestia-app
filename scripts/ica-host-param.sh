#!/bin/bash

# This script submits a governance proposal to modify the ICA host allow
# messages param.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

FROM="validator"

cat << 'EOF' > proposal.json
{
  "title": "Modify ICA host allow messages",
  "description": "Modify ICA host allow messages",
  "changes": [
    {
      "subspace": "icahost",
      "key": "AllowMessages",
      "value": "[\"/ibc.applications.transfer.v1.MsgTransfer\",\"/cosmos.bank.v1beta1.MsgSend\",\"/cosmos.staking.v1beta1.MsgDelegate\",\"/cosmos.staking.v1beta1.MsgBeginRedelegate\",\"/cosmos.staking.v1beta1.MsgUndelegate\",\"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation\",\"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress\",\"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward\",\"/cosmos.distribution.v1beta1.MsgFundCommunityPool\",\"/cosmos.gov.v1.MsgVote\",\"/cosmos.feegrant.v1beta1.MsgGrantAllowance\",\"/cosmos.feegrant.v1beta1.MsgRevokeAllowance\"]"
    }
  ],
  "deposit": "10000tia"
}
EOF

celestia-appd tx gov submit-legacy-proposal param-change proposal.json --from $FROM --fees 21000utia
