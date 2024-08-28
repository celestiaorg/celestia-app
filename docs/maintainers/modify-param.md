# Modify Param

This doc will guide you through the process of modifying a parameter via governance.

## Prerequisites

```shell
# Verify the current parameter value
$ celestia-appd query params subspace icahost AllowMessages
key: AllowMessages
subspace: icahost
value: '["*"]'
```

## Steps

```shell
# Create a proposal.json file
echo '{"title": "Modify ICA host allow messages", "description": "Modify ICA host allow messages", "changes": [{"subspace": "icahost", "key": "AllowMessages", "value": ["/ibc.applications.transfer.v1.MsgTransfer","/cosmos.bank.v1beta1.MsgSend","/cosmos.staking.v1beta1.MsgDelegate","/cosmos.staking.v1beta1.MsgBeginRedelegate","/cosmos.staking.v1beta1.MsgUndelegate","/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation","/cosmos.distribution.v1beta1.MsgSetWithdrawAddress","/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward","/cosmos.distribution.v1beta1.MsgFundCommunityPool","/cosmos.gov.v1.MsgVote","/cosmos.feegrant.v1beta1.MsgGrantAllowance","/cosmos.feegrant.v1beta1.MsgRevokeAllowance"]}], "deposit": "10000000000utia"}' > proposal.json

# Export a variable for the key that will be used to submit the proposal
export FROM="validator"
export FEES="210000utia"
export GAS="auto"
export GAS_ADJUSTMENT="1.5"

# Submit the proposal
celestia-appd tx gov submit-legacy-proposal param-change proposal.json --from $FROM --fees $FEES --gas $GAS --gas-adjustment $GAS_ADJUSTMENT --yes

# Query the proposals
celestia-appd query gov proposals --output json | jq .

# Export a variable for the relevant proposal ID based on the output from the previous command
export PROPOSAL_ID=1

# Vote yes on the proposal
celestia-appd tx gov vote $PROPOSAL_ID yes --from $FROM --fees $FEES --gas $GAS --gas-adjustment $GAS_ADJUSTMENT --yes
```

## After the proposal passes

```shell
# Verify the parameter value changed
$ celestia-appd query params subspace icahost AllowMessages
key: AllowMessages
subspace: icahost
value: '["/ibc.applications.transfer.v1.MsgTransfer","/cosmos.bank.v1beta1.MsgSend","/cosmos.staking.v1beta1.MsgDelegate","/cosmos.staking.v1beta1.MsgBeginRedelegate","/cosmos.staking.v1beta1.MsgUndelegate","/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation","/cosmos.distribution.v1beta1.MsgSetWithdrawAddress","/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward","/cosmos.distribution.v1beta1.MsgFundCommunityPool","/cosmos.gov.v1.MsgVote","/cosmos.feegrant.v1beta1.MsgGrantAllowance","/cosmos.feegrant.v1beta1.MsgRevokeAllowance"]'
```
