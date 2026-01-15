package types

import "cosmossdk.io/collections"

const (
	ModuleName = "forwarding"
	StoreKey   = ModuleName
	ParamsKey  = "params"

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20

	EventTypeTokenForwarded     = "forwarding.token_forwarded"
	EventTypeForwardingComplete = "forwarding.forwarding_complete"
	EventTypeTokensStuck        = "forwarding.tokens_stuck"

	AttributeKeyForwardAddr     = "forward_addr"
	AttributeKeyDenom           = "denom"
	AttributeKeyAmount          = "amount"
	AttributeKeyMessageId       = "message_id"
	AttributeKeySuccess         = "success"
	AttributeKeyError           = "error"
	AttributeKeyDestDomain      = "dest_domain"
	AttributeKeyDestRecipient   = "dest_recipient"
	AttributeKeyTokensForwarded = "tokens_forwarded"
	AttributeKeyTokensFailed    = "tokens_failed"
	AttributeKeyModuleAddr      = "module_addr"
)

var ParamsPrefix = collections.NewPrefix(1)
