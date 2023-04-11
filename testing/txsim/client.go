package txsim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	protogrpc "github.com/gogo/protobuf/grpc"
	"github.com/tendermint/tendermint/rpc/client/http"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
	"github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// how often to poll the network for the latest height
const (
	DefaultPollTime = 3 * time.Second
	maxRetries      = 20
)

var errTimedOutWaitingForTx = errors.New("timed out waiting for tx to be committed (1 minute)")

// TxClient is a client for submitting transactions to one of several nodes.
type TxClient struct {
	rpcClients []*http.HTTP
	encCfg     encoding.Config
	chainID    string
	pollTime   time.Duration

	mtx         sync.Mutex
	sequence    int
	height      int64
	lastUpdated time.Time
}

func NewTxClient(ctx context.Context, encCfg encoding.Config, pollTime time.Duration, rpcEndpoints []string) (*TxClient, error) {
	if len(rpcEndpoints) == 0 {
		return nil, errors.New("must have at least one endpoint specified")
	}

	// setup all the rpc clients to communicate with full nodes
	rpcClients := make([]*http.HTTP, len(rpcEndpoints))
	var (
		err     error
		chainID string
		height  int64
	)
	for i, endpoint := range rpcEndpoints {
		rpcClients[i], err = http.New(endpoint, "/websocket")
		if err != nil {
			return nil, fmt.Errorf("error creating rpc client with endpoint %s: %w", endpoint, err)
		}

		// check that the node is up
		status, err := rpcClients[i].Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting status from rpc server %s: %w", endpoint, err)
		}

		// set the chainID
		if chainID == "" {
			chainID = status.NodeInfo.Network
		}

		// set the latest height
		if status.SyncInfo.EarliestBlockHeight > height {
			height = status.SyncInfo.EarliestBlockHeight
		}
	}
	return &TxClient{
		rpcClients:  rpcClients,
		encCfg:      encCfg,
		chainID:     chainID,
		pollTime:    pollTime,
		height:      height,
		lastUpdated: time.Now(),
	}, nil
}

func (tc *TxClient) Tx() sdkclient.TxBuilder {
	builder := tc.encCfg.TxConfig.NewTxBuilder()
	return builder
}

func (tc *TxClient) ChainID() string {
	return tc.chainID
}

func (tc *TxClient) Height() int64 {
	tc.mtx.Lock()
	defer tc.mtx.Unlock()
	return tc.height
}

func (tc *TxClient) updateHeight(newHeight int64) int64 {
	tc.mtx.Lock()
	defer tc.mtx.Unlock()
	if newHeight > tc.height {
		tc.height = newHeight
		tc.lastUpdated = time.Now()
		return newHeight
	}
	return tc.height
}

func (tc *TxClient) LastUpdated() time.Time {
	tc.mtx.Lock()
	defer tc.mtx.Unlock()
	return tc.lastUpdated
}

// WaitForNBlocks uses WaitForHeight to wait for the given number of blocks to
// be produced.
func (tc *TxClient) WaitForNBlocks(ctx context.Context, blocks int64) error {
	return tc.WaitForHeight(ctx, tc.Height()+blocks)
}

// WaitForHeight continually polls the network for the latest height. It is
// concurrently safe.
func (tc *TxClient) WaitForHeight(ctx context.Context, height int64) error {
	// check if we can immediately return
	if height <= tc.Height() {
		return nil
	}

	ticker := time.NewTicker(tc.pollTime)
	for {
		select {
		case <-ticker.C:
			// check if we've reached the target height
			if height <= tc.Height() {
				return nil
			}
			// check when the last time we polled to avoid concurrent processes
			// from polling the network too often
			if time.Since(tc.LastUpdated()) < tc.pollTime {
				continue
			}

			// ping a node for their latest height
			status, err := tc.Client().Status(ctx)
			if err != nil {
				return fmt.Errorf("error getting status from rpc server: %w", err)
			}

			latestHeight := tc.updateHeight(status.SyncInfo.LatestBlockHeight)
			// check if the new latest height is greater or equal than the target height
			if latestHeight >= height {
				return nil
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (tc *TxClient) WaitForTx(ctx context.Context, txID []byte) (*coretypes.ResultTx, error) {
	for i := 0; i < maxRetries; i++ {
		resp, err := tc.Client().Tx(ctx, txID, false)
		if err != nil {
			// tx still no longer exists
			if strings.Contains(err.Error(), "not found") {
				time.Sleep(tc.pollTime)
				continue
			}
			return nil, err
		}

		if resp.TxResult.Code != 0 {
			return nil, fmt.Errorf("non zero code delivering tx (%d): %s", resp.TxResult.Code, resp.TxResult.Log)
		}

		return resp, nil
	}
	return nil, errTimedOutWaitingForTx
}

// Client multiplexes the RPC clients
func (tc *TxClient) Client() *http.HTTP {
	tc.mtx.Lock()
	defer tc.mtx.Unlock()
	defer tc.next()
	return tc.rpcClients[tc.sequence]
}

// Broadcast encodes and broadcasts a transaction to the network. If CheckTx fails,
// the error will be returned. The method does not wait for the transaction to be
// included in a block.
func (tc *TxClient) Broadcast(ctx context.Context, txBuilder sdkclient.TxBuilder, blobs []*blob.Blob) (*coretypes.ResultTx, error) {
	tx, err := tc.encCfg.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("error encoding tx: %w", err)
	}

	// If blobs exist, these are bundled into the existing tx.
	if len(blobs) > 0 {
		txWithBlobs, err := types.MarshalBlobTx(tx, blobs...)
		if err != nil {
			return nil, err
		}
		tx = txWithBlobs
	}

	resp, err := tc.Client().BroadcastTxSync(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("broadcast commit: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("non zero code checking tx (%d): %s", resp.Code, resp.Log)
	}

	return tc.WaitForTx(ctx, resp.Hash)
}

func (tc *TxClient) next() {
	tc.sequence = (tc.sequence + 1) % len(tc.rpcClients)
}

type QueryClient struct {
	connections []*grpc.ClientConn
	sequence    int
}

func NewQueryClient(grpcEndpoints []string) (*QueryClient, error) {
	connections := make([]*grpc.ClientConn, len(grpcEndpoints))
	for idx, endpoint := range grpcEndpoints {
		conn, err := grpc.Dial(grpcEndpoints[0], grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("dialing %s: %w", endpoint, err)
		}
		connections[idx] = conn
	}

	return &QueryClient{
		connections: connections,
	}, nil
}

func (qc *QueryClient) next() {
	qc.sequence = (qc.sequence + 1) % len(qc.connections)
}

func (qc *QueryClient) Conn() protogrpc.ClientConn {
	defer qc.next()
	return qc.connections[qc.sequence]
}

func (qc *QueryClient) Bank() bank.QueryClient {
	defer qc.next()
	return bank.NewQueryClient(qc.connections[qc.sequence])
}

func (qc *QueryClient) Auth() auth.QueryClient {
	defer qc.next()
	return auth.NewQueryClient(qc.connections[qc.sequence])
}

func (qc *QueryClient) Close() error {
	var err error
	for _, conn := range qc.connections {
		err = conn.Close()
	}
	return err
}
