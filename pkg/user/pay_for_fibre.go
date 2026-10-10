package user

import (
	"context"
	"errors"
	"strings"
	"time"

	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
)

// payForFibreJob is a signed payment promise waiting to be submitted as a
// MsgPayForFibre by a tx queue worker.
type payForFibreJob struct {
	Promise             *fibretypes.PaymentPromise
	ValidatorSignatures [][]byte
}

// SubmitPayForFibreToQueue submits a signed payment promise to the parallel transaction queue and
// blocks until the MsgPayForFibre transaction is confirmed.
func (client *TxClient) SubmitPayForFibreToQueue(ctx context.Context, promise *fibretypes.PaymentPromise, validatorSigs [][]byte, opts ...TxOption) (*TxResponse, error) {
	resultsC := make(chan SubmissionResult, 1)
	client.QueuePayForFibre(ctx, resultsC, promise, validatorSigs, opts...)
	return awaitResult(ctx, resultsC)
}

// QueuePayForFibre submits a signed payment promise to the parallel transaction queue without
// blocking. The worker that picks it up signs the MsgPayForFibre with its own account. The result
// will be sent to the provided channel when the transaction is confirmed. The caller is responsible
// for creating and closing the result channel.
func (client *TxClient) QueuePayForFibre(ctx context.Context, resultC chan SubmissionResult, promise *fibretypes.PaymentPromise, validatorSigs [][]byte, opts ...TxOption) {
	client.queueJob(&SubmissionJob{
		PayForFibre: &payForFibreJob{Promise: promise, ValidatorSignatures: validatorSigs},
		Options:     opts,
		Ctx:         ctx,
		ResultsC:    resultC,
	})
}

// SubmitPayForFibre forms a MsgPayForFibre transaction from the signed payment promise, signs it
// with the default account, and submits it to the chain.
func (client *TxClient) SubmitPayForFibre(ctx context.Context, promise *fibretypes.PaymentPromise, validatorSigs [][]byte, opts ...TxOption) (*TxResponse, error) {
	return client.submitPayForFibre(ctx, client.DefaultAddress().String(), promise, validatorSigs, opts...)
}

// submitPayForFibre submits a MsgPayForFibre signed by the account with the given address. The
// payment promise may be signed by a different key than the account submitting the transaction.
func (client *TxClient) submitPayForFibre(ctx context.Context, signer string, promise *fibretypes.PaymentPromise, validatorSigs [][]byte, opts ...TxOption) (*TxResponse, error) {
	if promise == nil {
		return nil, errors.New("payment promise is nil")
	}
	msg := &fibretypes.MsgPayForFibre{
		Signer:              signer,
		PaymentPromise:      *promise,
		ValidatorSignatures: validatorSigs,
	}

	resp, err := retryPFFBroadcast(ctx, func(ctx context.Context) (*sdktypes.TxResponse, error) {
		return client.BroadcastTx(ctx, []sdktypes.Msg{msg}, opts...)
	})
	if err != nil {
		return nil, err
	}

	return client.ConfirmTx(ctx, resp.TxHash)
}

// pffBroadcastAttempts and pffBroadcastRetryDelay bound the re-broadcast of a
// PayForFibre rejected because the promise's signing height is not yet
// committed app-side.
const (
	pffBroadcastAttempts   = 4
	pffBroadcastRetryDelay = 500 * time.Millisecond
)

// isMissingHistoricalInfo reports whether a broadcast rejection means the
// promise's signing height is not yet committed app-side. The head is
// observable from CometBFT's blockstore before the app finishes executing the
// block, so this rejection is transient: the tx verifies once the app catches
// up, moments later. See #7774.
func isMissingHistoricalInfo(err error) bool {
	return err != nil && strings.Contains(err.Error(), "failed to get historical validator set")
}

// retryPFFBroadcast broadcasts a PayForFibre, re-broadcasting after a short
// delay while the rejection is the transient missing-historical-info one.
func retryPFFBroadcast(ctx context.Context, broadcast func(context.Context) (*sdktypes.TxResponse, error)) (*sdktypes.TxResponse, error) {
	resp, err := broadcast(ctx)
	for attempt := 1; attempt < pffBroadcastAttempts && isMissingHistoricalInfo(err); attempt++ {
		select {
		case <-ctx.Done():
			return resp, err
		case <-time.After(pffBroadcastRetryDelay):
		}
		resp, err = broadcast(ctx)
	}
	return resp, err
}
