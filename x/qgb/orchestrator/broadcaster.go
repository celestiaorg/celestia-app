package orchestrator

import (
	"context"
	"fmt"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"sync"
)

var _ Broadcaster = &broadcaster{}

type Broadcaster interface {
	BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error)
}

type broadcaster struct {
	mutex   *sync.Mutex
	signer  *paytypes.KeyringSigner
	qgbGrpc *grpc.ClientConn
}

func NewBroadcaster(qgbGrpcAddr string, signer *paytypes.KeyringSigner) (Broadcaster, error) {
	qgbGrpc, err := grpc.Dial(qgbGrpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return &broadcaster{
		mutex:   &sync.Mutex{},
		signer:  signer,
		qgbGrpc: qgbGrpc,
	}, nil
}

func (bc *broadcaster) BroadcastTx(ctx context.Context, msg sdk.Msg) (string, error) {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()
	err := bc.signer.QueryAccountNumber(ctx, bc.qgbGrpc)
	if err != nil {
		return "", err
	}

	builder := bc.signer.NewTxBuilder()
	// TODO make gas limit configurable
	builder.SetGasLimit(9999999999999)
	// TODO: update this api
	// via https://github.com/celestiaorg/celestia-app/pull/187/commits/37f96d9af30011736a3e6048bbb35bad6f5b795c
	tx, err := bc.signer.BuildSignedTx(builder, msg)
	if err != nil {
		return "", err
	}

	rawTx, err := bc.signer.EncodeTx(tx)
	if err != nil {
		return "", err
	}

	// TODO  check if we can move this outside of the paytypes
	resp, err := paytypes.BroadcastTx(ctx, bc.qgbGrpc, 1, rawTx)
	if err != nil {
		return "", err
	}

	if resp.TxResponse.Code != 0 {
		return "", fmt.Errorf("failure to broadcast tx: %s", resp.TxResponse.RawLog)
	}

	return resp.TxResponse.TxHash, nil
}
