package fibre

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

// escrowReservation is a single Put's handle on its escrow admission. The zero
// value (returned when AutoFund is disabled) makes settle/release no-ops.
type escrowReservation struct {
	ledger    *escrowLedger
	hash      string
	settled   bool
	broadcast bool
}

// admitEscrow reserves blob's settlement cost against txClient's escrow before
// signing/uploading, so concurrent in-flight promises can never collectively
// overcommit the account (the failure mode neither the server's per-promise
// check nor a fixed upfront deposit prevents). It blocks — auto-funding as
// needed — until the budget is available, then kicks off background upkeep off
// the hot path. Returns the zero reservation when AutoFund is disabled.
func (c *Client) admitEscrow(ctx context.Context, txClient *user.TxClient, blob *Blob) (escrowReservation, error) {
	if !c.Config.Escrow.AutoFund {
		return escrowReservation{}, nil
	}
	ledger := c.escrowLedgerFor(txClient)
	hash := c.newEscrowReservationKey(blob.ID())
	amount := types.PaymentAmount(uint32(blob.UploadSize())).Amount
	if err := ledger.ensureSeeded(ctx); err != nil {
		return escrowReservation{}, fmt.Errorf("seeding escrow ledger: %w", err)
	}
	if !ledger.reserve(hash, amount) {
		if err := ledger.waitForBudget(ctx, hash, amount); err != nil {
			return escrowReservation{}, fmt.Errorf("waiting for escrow budget: %w", err)
		}
	}
	c.maintainEscrowAsync(ledger)
	return escrowReservation{ledger: ledger, hash: hash}, nil
}

// settle marks the reservation paid on-chain (the PFF was confirmed): the escrow
// was debited, so the reservation shrinks both the reserved total and the
// tracked balance.
func (r *escrowReservation) settle() {
	if r.ledger != nil {
		r.ledger.releaseSettled(r.hash)
		r.settled = true
	}
}

// release returns the budget for a reservation that was aborted before the PFF
// was broadcast (encoding/upload/broadcast failed, so the funds were never
// spent). Safe to defer unconditionally: it is a no-op once the reservation is
// settled or the PFF has been broadcast (uncertain outcome, left for reconcile).
func (r *escrowReservation) release() {
	if r.ledger != nil && !r.settled && !r.broadcast {
		r.ledger.releaseUnsettled(r.hash)
	}
}

// Put uploads given data to the Fibre network.
// It encodes the data into a [Blob], calls [Client.Upload] to upload it,
// and submits a MsgPayForFibre transaction using the provided [user.TxClient].
//
// TODO(@Wondertan): This does not belong here. Fibre protocol in it's core doesn't need to know about transactions.
// Furthermore, this function cannot be generalized for all the cases with fee grants, multiple key managements, etc.
// And users are strongly advised to use [fibre.Upload] with custom TX submission logic instead, ideally batching multiple blobs in a single PFF.
func Put(ctx context.Context, c *Client, txClient *user.TxClient, ns share.Namespace, data []byte) (result PutResult, err error) {
	ctx, span := c.tracer.Start(ctx, "fibre.Client.Put",
		trace.WithAttributes(
			attribute.String("namespace", ns.String()),
			attribute.Int("data_size", len(data)),
		),
	)
	defer span.End()

	// encoding section
	blob, err := NewBlob(data, DefaultBlobConfigV0())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to encode blob")
		return result, err
	}
	defer blob.Free()

	blobID := blob.ID()
	span.AddEvent("blob_encoded", trace.WithAttributes(
		attribute.String("blob_id", blobID.String()),
		attribute.Int("row_size", blob.RowSize()),
	))

	// Escrow admission: reserve this promise's settlement cost before signing and
	// uploading. Released as settled once the PFF is confirmed, or unsettled (via
	// the deferred release) on any earlier error. No-op when AutoFund is disabled.
	reservation, err := c.admitEscrow(ctx, txClient, blob)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "escrow admission failed")
		return result, err
	}
	defer reservation.release()

	signedPromise, err := c.Upload(ctx, ns, blob, WithKeyName(txClient.DefaultAccountName()))
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
	reservation.broadcast = true

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

	// The PFF is on-chain: the escrow was debited, so settle the reservation.
	reservation.settle()

	span.SetStatus(codes.Ok, "")
	return PutResult{
		BlobID:              blobID,
		ValidatorSignatures: signedPromise.ValidatorSignatures,
		TxHash:              txResp.TxHash,
		Height:              uint64(txResp.Height),
	}, nil
}
