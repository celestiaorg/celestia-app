package payment

import (
	"context"
	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdk_tx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/nmt/namespace"
)

var (
	// shareSizes includes all the possible share sizes of the given data
	// that the signer must sign over.
	shareSizes = []uint64{16, 32, 64, 128}
)

// SubmitPayForData constructs, signs and synchronously submits a PayForData
// transaction, returning a sdk.TxResponse upon submission.
func SubmitPayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	data []byte,
	gasLim uint64,
	opts ...types.TxBuilderOption,
) (*sdk.TxResponse, error) {
	pfd, err := BuildPayForData(ctx, signer, conn, nID, data, gasLim)
	if err != nil {
		return nil, err
	}

	signed, err := SignPayForData(signer, pfd, append(opts, types.SetGasLimit(gasLim))...)
	if err != nil {
		return nil, err
	}

	rawTx, err := signer.EncodeTx(signed)
	if err != nil {
		return nil, err
	}

	txResp, err := types.BroadcastTx(ctx, conn, sdk_tx.BroadcastMode_BROADCAST_MODE_BLOCK, rawTx)
	if err != nil {
		return nil, err
	}
	return txResp.TxResponse, nil
}

// BuildPayForData constructs a PayForData transaction.
func BuildPayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	message []byte,
	gasLim uint64,
) (*types.MsgWirePayForData, error) {
	// create the raw WirePayForData transaction
	wpfd, err := types.NewWirePayForData(nID, message, shareSizes...)
	if err != nil {
		return nil, err
	}

	// query for account information necessary to sign a valid tx
	err = signer.QueryAccountNumber(ctx, conn)
	if err != nil {
		return nil, err
	}

	// generate the signatures for each `MsgPayForData` using the `KeyringSigner`,
	// then set the gas limit for the tx
	gasLimOption := types.SetGasLimit(gasLim)
	err = wpfd.SignShareCommitments(signer, gasLimOption)
	if err != nil {
		return nil, err
	}

	return wpfd, nil
}

// SignPayForData signs a PayForData transaction.
func SignPayForData(
	signer *types.KeyringSigner,
	pfd *types.MsgWirePayForData,
	opts ...types.TxBuilderOption,
) (signing.Tx, error) {
	// Build and sign the final `WirePayForData` tx that now contains the signatures
	// for potential `MsgPayForData`s
	builder := signer.NewTxBuilder()
	for _, opt := range opts {
		opt(builder)
	}
	return signer.BuildSignedTx(
		builder,
		pfd,
	)
}
