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

// SubmitPayForData builds, signs, and synchronously submits a PayForData
// transaction. It returns a sdk.TxResponse after submission.
func SubmitPayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	data []byte,
	gasLim uint64,
	opts ...types.TxBuilderOption,
) (*sdk.TxResponse, error) {
	opts = append(opts, types.SetGasLimit(gasLim))

	pfd, err := BuildPayForData(ctx, signer, conn, nID, data, opts...)
	if err != nil {
		return nil, err
	}

	signed, err := SignPayForData(signer, pfd, opts...)
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

// BuildPayForData builds a PayForData transaction.
func BuildPayForData(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	message []byte,
	opts ...types.TxBuilderOption,
) (*types.MsgWirePayForData, error) {
	// create the raw WirePayForData transaction
	wpfd, err := types.NewWirePayForData(nID, message, types.AllSquareSizes(len(message))...)
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
	err = wpfd.SignShareCommitments(signer, opts...)
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
