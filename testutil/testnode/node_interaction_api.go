package testnode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/x/payment"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
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
func (c *Context) PostData(account string, ns, msg []byte) (*sdk.TxResponse, error) {
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
	pfd, err := payment.BuildPayForData(
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

	signed, err := payment.SignPayForData(signer, pfd, opts...)
	if err != nil {
		return nil, err
	}

	rawTx, err := signer.EncodeTx(signed)
	if err != nil {
		return nil, err
	}

	res, err := c.BroadcastTxCommit(rawTx)
	if err != nil {
		return nil, err
	}
	if res.Code != abci.CodeTypeOK {
		return nil, fmt.Errorf("failure to broadcast tx sync: %s", res.RawLog)
	}

	return res, nil
}

// FillBlock will create and submit enough PFD txs to fill a block to a specific
// square size. It uses a crude mechanism to estimate the number of txs needed
// by creating message that each take up a single row, and creating squareSize
// -2 of those PFDs.
func FillBlock(cctx client.Context, squareSize int, accounts []string) ([]*sdk.TxResponse, error) {
	// todo: fix or debug this after cherry-picking this commit to a branch w/ non-interactive defaults
	msgCount := (squareSize / 4)
	if len(accounts) < msgCount {
		return nil, fmt.Errorf("more funded accounts are needed: want >=%d have %d", msgCount, len(accounts))
	}

	// todo: fix or debug this after cherry-picking this commit to a branch w/ non-interactive defaults
	msgSize := ((squareSize / 2) * appconsts.SparseShareContentSize) - 300

	opts := []types.TxBuilderOption{
		types.SetGasLimit(100000000000000),
	}

	results := make([]*sdk.TxResponse, msgCount)
	for i := 0; i < msgCount; i++ {
		// use the key for accounts[i] to create a singer used for a single PFD
		signer := types.NewKeyringSigner(cctx.Keyring, accounts[i], cctx.ChainID)

		rec := signer.GetSignerInfo()
		addr, err := rec.GetAddress()
		if err != nil {
			return nil, err
		}

		acc, seq, err := cctx.AccountRetriever.GetAccountNumberSequence(cctx, addr)
		if err != nil {
			return nil, err
		}

		signer.SetAccountNumber(acc)
		signer.SetSequence(seq)

		// create a random msg per row
		pfd, err := payment.BuildPayForData(
			context.TODO(),
			signer,
			cctx.GRPCClient,
			namespace.RandomMessageNamespace(),
			tmrand.Bytes(msgSize),
			opts...,
		)
		if err != nil {
			return nil, err
		}

		signed, err := payment.SignPayForData(signer, pfd, opts...)
		if err != nil {
			return nil, err
		}

		rawTx, err := signer.EncodeTx(signed)
		if err != nil {
			return nil, err
		}

		res, err := cctx.BroadcastTxCommit(rawTx)
		if err != nil {
			return nil, err
		}
		if res.Code != abci.CodeTypeOK {
			return nil, fmt.Errorf("failure to broadcast tx sync: %s %d", res.RawLog, i)
		}
		results[i] = res
	}
	return nil, nil
}
