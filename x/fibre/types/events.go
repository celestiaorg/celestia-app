package types

import (
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
)

var (
	EventTypeDepositToEscrow            = proto.MessageName(&EventDepositToEscrow{})
	EventTypeWithdrawFromEscrowRequest  = proto.MessageName(&EventWithdrawFromEscrowRequest{})
	EventTypeWithdrawFromEscrowExecuted = proto.MessageName(&EventWithdrawFromEscrowExecuted{})
	EventTypePayForFibre                = proto.MessageName(&EventPayForFibre{})
	EventTypePaymentPromiseTimeout      = proto.MessageName(&EventPaymentPromiseTimeout{})
	EventTypeUpdateFibreParams          = proto.MessageName(&EventUpdateFibreParams{})
	EventTypeProcessedPaymentPruned     = proto.MessageName(&EventProcessedPaymentPruned{})
)

// NewEventDepositToEscrow returns a new EventDepositToEscrow
func NewEventDepositToEscrow(signer string, amount sdk.Coin) *EventDepositToEscrow {
	return &EventDepositToEscrow{
		Signer: signer,
		Amount: amount,
	}
}

// NewEventWithdrawFromEscrowRequest returns a new EventWithdrawFromEscrowRequest
func NewEventWithdrawFromEscrowRequest(signer string, amount sdk.Coin, requestedAt time.Time, availableAt time.Time) *EventWithdrawFromEscrowRequest {
	return &EventWithdrawFromEscrowRequest{
		Signer:      signer,
		Amount:      amount,
		RequestedAt: requestedAt,
		AvailableAt: availableAt,
	}
}

// NewEventWithdrawFromEscrowExecuted returns a new EventWithdrawFromEscrowExecuted
func NewEventWithdrawFromEscrowExecuted(signer string, amount sdk.Coin) *EventWithdrawFromEscrowExecuted {
	return &EventWithdrawFromEscrowExecuted{
		Signer: signer,
		Amount: amount,
	}
}

// NewEventPayForFibre returns a new EventPayForFibre
func NewEventPayForFibre(signer string, namespace []byte, commitment []byte, validatorCount uint32) *EventPayForFibre {
	return &EventPayForFibre{
		Signer:         signer,
		Namespace:      namespace,
		Commitment:     commitment,
		ValidatorCount: validatorCount,
	}
}

// PayForFibreEvent constructs the same sorted JSON attributes as the SDK's
// TypedEventToEvent without a protobuf JSON marshal and map decode per payment.
func PayForFibreEvent(signer string, namespace, commitment []byte, validatorCount uint32) sdk.Event {
	commitmentJSON, _ := json.Marshal(commitment)
	namespaceJSON, _ := json.Marshal(namespace)
	signerJSON, _ := json.Marshal(signer)
	// proto.MessageName at call time, not the EventType... vars above: those
	// are package-level variables initialised before the generated init()
	// registers the types, so they are empty strings. See the equivalence test.
	return sdk.NewEvent(proto.MessageName(&EventPayForFibre{}),
		sdk.NewAttribute("commitment", string(commitmentJSON)),
		sdk.NewAttribute("namespace", string(namespaceJSON)),
		sdk.NewAttribute("signer", string(signerJSON)),
		sdk.NewAttribute("validator_count", strconv.FormatUint(uint64(validatorCount), 10)),
	)
}

// NewEventPaymentPromiseTimeout returns a new EventPaymentPromiseTimeout
func NewEventPaymentPromiseTimeout(processor string, escrowSigner string, paymentPromiseHash []byte) *EventPaymentPromiseTimeout {
	return &EventPaymentPromiseTimeout{
		Processor:          processor,
		EscrowSigner:       escrowSigner,
		PaymentPromiseHash: paymentPromiseHash,
	}
}

// NewEventUpdateFibreParams returns a new EventUpdateFibreParams
func NewEventUpdateFibreParams(authority string, params Params) *EventUpdateFibreParams {
	return &EventUpdateFibreParams{
		Signer: authority,
		Params: params,
	}
}

// NewEventProcessedPaymentPruned returns a new EventProcessedPaymentPruned
func NewEventProcessedPaymentPruned(paymentPromiseHash []byte, processedAt time.Time) *EventProcessedPaymentPruned {
	return &EventProcessedPaymentPruned{
		PaymentPromiseHash: paymentPromiseHash,
		ProcessedAt:        processedAt,
	}
}
