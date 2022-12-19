package blob

import (
	"context"

	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdk_tx "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/nmt/namespace"
	coretypes "github.com/tendermint/tendermint/types"
)

// SubmitPayForBlob builds, signs, and synchronously submits a PayForBlob
// transaction. It returns a sdk.TxResponse after submission.
func SubmitPayForBlob(
	ctx context.Context,
	signer *types.KeyringSigner,
	conn *grpc.ClientConn,
	nID namespace.ID,
	blob []byte,
	shareVersion uint8,
	gasLim uint64,
	opts ...types.TxBuilderOption,
) (*sdk.TxResponse, error) {
	opts = append(opts, types.SetGasLimit(gasLim))
	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		return nil, err
	}
	msg, err := types.NewMsgPayForBlob(
		addr.String(),
		nID,
		blob,
	)
	if err != nil {
		return nil, err
	}
	err = signer.QueryAccountNumber(ctx, conn)
	if err != nil {
		return nil, err
	}
	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	if err != nil {
		return nil, err
	}
	wblob, err := types.NewBlob(msg.NamespaceId, blob)
	if err != nil {
		return nil, err
	}
	rawTx, err := signer.EncodeTx(stx)
	if err != nil {
		return nil, err
	}
	blobTx, err := coretypes.MarshalBlobTx(rawTx, wblob)
	if err != nil {
		return nil, err
	}
	txResp, err := types.BroadcastTx(ctx, conn, sdk_tx.BroadcastMode_BROADCAST_MODE_BLOCK, blobTx)
	if err != nil {
		return nil, err
	}
	return txResp.TxResponse, nil
}
