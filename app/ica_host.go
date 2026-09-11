package app

// IcaAllowMessages returns the list of messages that are allowed to be sent via ICA.
//
// Adding a message that dispatches further messages would let one ICA packet
// execute more than countExecutableMsgs counts, which counts a payload's entries
// without looking inside them. MsgModuleQuerySafe is the likeliest such message,
// since it exists for ICA controllers to query the host, but it dispatches one
// query per request. See TestIcaAllowMessagesExcludeFanOutMsgs.
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
