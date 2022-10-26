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

// SubmitWirePayForData constructs, signs, and synchronously submits a WirePayForData
// transaction, returning a sdk.TxResponse upon submission.
func SubmitWirePayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	data []byte,
	gasLim uint64,
	opts ...types.TxBuilderOption,
) (*sdk.TxResponse, error) {
	opts = append(opts, types.SetGasLimit(gasLim))

	wpfd, err := BuildWirePayForData(ctx, signer, conn, nID, data, opts...)
	if err != nil {
		return nil, err
	}

	signed, err := SignWirePayForData(signer, wpfd, opts...)
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

// BuildWirePayForData returns a MsgWirePayForData
func BuildWirePayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	message []byte,
	opts ...types.TxBuilderOption,
) (*types.MsgWirePayForData, error) {
	// create the raw WirePayForData
	wpfd, err := types.NewWirePayForData(nID, message, types.AllSquareSizes(len(message))...)
	if err != nil {
		return nil, err
	}

	// query for account information necessary to sign a valid tx
	err = signer.QueryAccountNumber(ctx, conn)
	if err != nil {
		return nil, err
	}

	// sign the message share commitments using the signer
	err = wpfd.SignShareCommitments(signer, opts...)
	if err != nil {
		return nil, err
	}

	return wpfd, nil
}

// SignWirePayForData signs a WirePayForData transaction.
func SignWirePayForData(
	signer *types.KeyringSigner,
	wpfd *types.MsgWirePayForData,
	opts ...types.TxBuilderOption,
) (signing.Tx, error) {
	// Build and sign the final `WirePayForData` tx
	builder := signer.NewTxBuilder()
	for _, opt := range opts {
		opt(builder)
	}
	return signer.BuildSignedTx(
		builder,
		wpfd,
	)
}
