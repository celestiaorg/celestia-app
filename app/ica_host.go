package app

// IcaAllowMessages returns the list of messages that are allowed to be sent via ICA.
func IcaAllowMessages() []string {
	return []string{
		"/ibc.applications.transfer.v1.MsgTransfer",
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgBeginRedelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation",
		"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
		"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
		"/cosmos.distribution.v1beta1.MsgFundCommunityPool",
		"/cosmos.gov.v1.MsgVote",
		"/cosmos.feegrant.v1beta1.MsgGrantAllowance",
		"/cosmos.feegrant.v1beta1.MsgRevokeAllowance",
	}
}
