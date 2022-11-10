package testnode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	blob "github.com/celestiaorg/celestia-app/x/blob"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

type Context struct {
	rootCtx context.Context
	client.Context
}

func (c *Context) GoContext() context.Context {
	return c.rootCtx
}

// LatestHeight returns the latest height of the network or an error if the
// query fails.
func (c *Context) LatestHeight() (int64, error) {
	status, err := c.Client.Status(c.GoContext())
	if err != nil {
		return 0, err
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func (c *Context) WaitForHeightWithTimeout(h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(c.rootCtx, t)
	defer cancel()

	var latestHeight int64
	for {
		select {
		case <-ctx.Done():
			return latestHeight, errors.New("timeout exceeded waiting for block")
		case <-ticker.C:
			latestHeight, err := c.LatestHeight()
			if err != nil {
				return 0, err
			}
			if latestHeight >= h {
				return latestHeight, nil
			}
		}
	}
}

// WaitForHeight performs a blocking check where it waits for a block to be
// committed after a given block. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest height queried is returned.
func (c *Context) WaitForHeight(h int64) (int64, error) {
	return c.WaitForHeightWithTimeout(h, 10*time.Second)
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func (c *Context) WaitForNextBlock() error {
	lastBlock, err := c.LatestHeight()
	if err != nil {
		return err
	}

	_, err = c.WaitForHeight(lastBlock + 1)
	if err != nil {
		return err
	}

	return err
}

// PostData will create and submit PFD transaction containing the message and
// namespace. This function blocks until the PFD has been included in a block
// and returns an error if the transaction is invalid or is rejected by the
// mempool.
func (c *Context) PostData(account, broadcastMode string, ns, msg []byte) (*sdk.TxResponse, error) {
	opts := []types.TxBuilderOption{
		types.SetGasLimit(100000000000000),
	}

	// use the key for accounts[i] to create a singer used for a single PFD
	signer := types.NewKeyringSigner(c.Keyring, account, c.ChainID)

	rec := signer.GetSignerInfo()
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	acc, seq, err := c.AccountRetriever.GetAccountNumberSequence(c.Context, addr)
	if err != nil {
		return nil, err
	}

	signer.SetAccountNumber(acc)
	signer.SetSequence(seq)

	// create a random msg per row
	pfd, err := blob.BuildPayForData(
		c.rootCtx,
		signer,
		c.GRPCClient,
		ns,
		msg,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	signed, err := blob.SignPayForData(signer, pfd, opts...)
	if err != nil {
		return nil, err
	}

	rawTx, err := signer.EncodeTx(signed)
	if err != nil {
		return nil, err
	}
	var res *sdk.TxResponse
	switch broadcastMode {
	case flags.BroadcastSync:
		res, err = c.BroadcastTxSync(rawTx)
	case flags.BroadcastAsync:
		res, err = c.BroadcastTxAsync(rawTx)
	case flags.BroadcastBlock:
		res, err = c.BroadcastTxCommit(rawTx)
	default:
		return nil, fmt.Errorf("unsupported broadcast mode %s; supported modes: sync, async, block", c.BroadcastMode)
	}
	if err != nil {
		return nil, err
	}
	if res.Code != abci.CodeTypeOK {
		return res, fmt.Errorf("failure to broadcast tx sync: %s", res.RawLog)
	}

	return res, nil
}

// FillBlock creates and submits a single transaction that is large enough to
// create a square of the desired size. broadcast mode indicates if the tx
// should be submitted async, sync, or block. (see flags.BroadcastModeSync). If
// broadcast mode is the string zero value, then it will be set to block. This
// function does not perform checks on the passed squaresize arg, and only works
// with squaresize >= 2. TODO: perform checks (is a power of 2 and is > 2) on
// the passed squaresize arg
func (c *Context) FillBlock(squareSize int, accounts []string, broadcastMode string) (*sdk.TxResponse, error) {
	if broadcastMode == "" {
		broadcastMode = flags.BroadcastBlock
	}
	maxShareCount := squareSize * squareSize
	// we use a formula to guarantee that the tx is the exact size needed to force a specific square size.
	msgSize := (maxShareCount - (2 * squareSize)) * appconsts.SparseShareContentSize
	// this last patch allows for the formula above to work on a square size of
	// 2.
	if msgSize < 1 {
		msgSize = 1
	}
	return c.PostData(accounts[0], broadcastMode, namespace.RandomMessageNamespace(), tmrand.Bytes(msgSize))
}
