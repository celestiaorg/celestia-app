package fibre

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// PutResult contains the result of a [Client.Put] operation.
type PutResult struct {
	// BlobID uniquely identifies the uploaded blob.
	BlobID BlobID
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
// and submits a MsgPayForFibre transaction using the provided [user.TxClient].
//
// TODO(@Wondertan): This does not belong here. Fibre protocol in it's core doesn't need to know about transactions.
// Furthermore, this function cannot be generalized for all the cases with fee grants, multiple key managements, etc.
// And users are strongly advised to use [fibre.Upload] with custom TX submission logic instead, ideally batching multiple blobs in a single PFF.
func Put(ctx context.Context, c *Client, txClient *user.TxClient, ns share.Namespace, data []byte) (result PutResult, err error) {
	start := time.Now()
	c.metrics.putInFlight.Add(ctx, 1)

	ctx, span := c.tracer.Start(ctx, "fibre.Client.Put",
		trace.WithAttributes(
			attribute.String("namespace", ns.String()),
			attribute.Int("data_size", len(data)),
		),
	)
	defer span.End()
	defer func() {
		c.metrics.putInFlight.Add(ctx, -1)
		c.metrics.putDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.Bool("success", err == nil)))
	}()

	// encoding section
	blob, err := NewBlob(data, DefaultBlobConfigV0())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to encode blob")
		return result, err
	}

	blobID := blob.ID()
	span.AddEvent("blob_encoded", trace.WithAttributes(
		attribute.String("blob_id", blobID.String()),
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
	promiseProto, err := signedPromise.ToProto()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to convert payment promise to proto")
		return result, fmt.Errorf("converting payment promise to proto: %w", err)
	}
	signerAddr := txClient.DefaultAddress()
	msg := &types.MsgPayForFibre{
		Signer:              signerAddr.String(),
		PaymentPromise:      *promiseProto,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
	}

	broadcastResp, err := txClient.BroadcastTx(ctx, []sdk.Msg{msg})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to broadcast PayForFibre transaction")
		return result, fmt.Errorf("broadcasting PayForFibre transaction: %w", err)
	}
	span.AddEvent("pff_broadcasted", trace.WithAttributes(
		attribute.String("pff_hash", broadcastResp.TxHash),
	))

	// confirm transaction inclusion
	txResp, err := txClient.ConfirmTx(ctx, broadcastResp.TxHash)
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
		BlobID:              blobID,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
		TxHash:              txResp.TxHash,
		Height:              uint64(txResp.Height),
	}, nil
}
