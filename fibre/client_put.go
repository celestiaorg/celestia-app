package fibre

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/celestiaorg/go-square/v3/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PutResult contains the result of a [Client.Put] operation.
type PutResult struct {
	// Commitment is the commitment to the [Blob].
	Commitment Commitment
	// ValidatorSignatures are ed25519 signatures over the [PaymentPromise] sign bytes.
	ValidatorSignatures [][]byte
	// TTL is the time-to-live for the [Blob].
	TTL time.Time
	// TxHash is the transaction hash of the [types.MsgPayForFibre].
	TxHash string
	// Height is the block height where the [types.MsgPayForFibre] transaction was included.
	Height uint64
}

// Put uploads given data to the Fibre network.
// It encodes the data into a [Blob], calls [Client.Upload] to upload it,
// and submits a MsgPayForFibre transaction.
func (c *Client) Put(ctx context.Context, ns share.Namespace, data []byte) (result PutResult, err error) {
	ctx, span := c.tracer.Start(ctx, "fibre.Client.Put",
		trace.WithAttributes(
			attribute.String("namespace", ns.String()),
			attribute.Int("data_size", len(data)),
		),
	)
	defer span.End()

	// encoding section
	blob, err := NewBlob(data, c.cfg.BlobConfig)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to encode blob")
		return result, err
	}

	commitment := blob.Commitment()
	span.AddEvent("blob_encoded", trace.WithAttributes(
		attribute.String("commitment", commitment.String()),
		attribute.Int("row_size", blob.RowSize()),
	))

	signedPromise, err := c.Upload(ctx, ns, blob)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload blob")
		return result, err
	}
	span.AddEvent("blob_uploaded", trace.WithAttributes(
		attribute.Int("sigs_amount", len(signedPromise.ValidatorSignatures)),
	))

	// broadcast PayForFibre transaction
	signerAddr := c.txClient.DefaultAddress()
	msg := &types.MsgPayForFibre{
		Signer:              signerAddr.String(),
		PaymentPromise:      *signedPromise.ToProto(),
		ValidatorSignatures: signedPromise.ValidatorSignatures,
	}

	broadcastResp, err := c.txClient.BroadcastTx(ctx, []sdk.Msg{msg})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to broadcast PayForFibre transaction")
		return result, fmt.Errorf("broadcasting PayForFibre transaction: %w", err)
	}
	span.AddEvent("pff_broadcasted", trace.WithAttributes(
		attribute.String("pff_hash", broadcastResp.TxHash),
	))

	// confirm transaction inclusion
	txResp, err := c.txClient.ConfirmTx(ctx, broadcastResp.TxHash)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to confirm PayForFibre transaction")
		return result, fmt.Errorf("confirming PayForFibre transaction: %w", err)
	}
	span.AddEvent("pff_confirmed", trace.WithAttributes(
		attribute.Int64("height", txResp.Height),
	))

	span.SetStatus(codes.Ok, "")
	return PutResult{
		Commitment:          commitment,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
		TxHash:              txResp.TxHash,
		Height:              uint64(txResp.Height),
	}, nil
}
