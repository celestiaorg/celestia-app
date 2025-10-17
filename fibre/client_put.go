package fibre

import (
	"context"
	"time"

	"github.com/celestiaorg/go-square/v3/share"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PFFConfirmation contains information about the PayForFibre transaction.
type PFFConfirmation struct {
	// TxHash is the transaction hash of the PayForFibre message.
	TxHash string
	// Height is the block height where the PayForFibre transaction was included.
	Height uint64
}

// PutResult contains the result of a [Client.Put] operation.
type PutResult struct {
	// Commitment is the commitment to the blob.
	Commitment Commitment
	// ValidatorSignatures are secp256k1 signatures over the [PaymentPromise] sign bytes.
	ValidatorSignatures [][]byte
	// TTL is the time-to-live for the blob.
	TTL time.Time
	// PFFConfirmation contains the transaction hash and height of the PayForFibre message.
	PFFConfirmation PFFConfirmation
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
	span.AddEvent("encoded", trace.WithAttributes(
		attribute.String("commitment", commitment.String()),
		attribute.Int("row_size", blob.RowSize()),
	))

	signedPromise, err := c.Upload(ctx, ns, blob)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload blob")
		return result, err
	}

	// TODO(@Wondertan): Submit and await inclusion of MsgPayForFibre transaction through txClient.

	span.SetStatus(codes.Ok, "")
	result = PutResult{
		Commitment:          commitment,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
	}
	return result, nil
}
