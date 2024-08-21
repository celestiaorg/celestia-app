package testnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmconfig "github.com/tendermint/tendermint/config"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

const (
	DefaultTimeout = 30 * time.Second
)

type Context struct {
	goContext context.Context
	client.Context
	apiAddress string
}

func NewContext(goContext context.Context, keyring keyring.Keyring, tmConfig *tmconfig.Config, chainID, apiAddress string) Context {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	clientContext := client.Context{}.
		WithKeyring(keyring).
		WithHomeDir(tmConfig.RootDir).
		WithChainID(chainID).
		WithInterfaceRegistry(config.InterfaceRegistry).
		WithCodec(config.Codec).
		WithLegacyAmino(config.Amino).
		WithTxConfig(config.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{})

	return Context{goContext: goContext, Context: clientContext, apiAddress: apiAddress}
}

func (c *Context) GoContext() context.Context {
	return c.goContext
}

// GenesisTime returns the genesis block time.
func (c *Context) GenesisTime() (time.Time, error) {
	height := int64(1)
	status, err := c.Client.Block(c.GoContext(), &height)
	if err != nil {
		return time.Unix(0, 0), err
	}

	return status.Block.Time, nil
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

// LatestTimestamp returns the latest timestamp of the network or an error if the
// query fails.
func (c *Context) LatestTimestamp() (time.Time, error) {
	current, err := c.Client.Block(c.GoContext(), nil)
	if err != nil {
		return time.Unix(0, 0), err
	}
	return current.Block.Time, nil
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func (c *Context) WaitForHeightWithTimeout(h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(c.goContext, t)
	defer cancel()

	var (
		latestHeight int64
		err          error
	)
	for {
		select {
		case <-ctx.Done():
			if c.goContext.Err() != nil {
				return latestHeight, c.goContext.Err()
			}
			return latestHeight, fmt.Errorf("timeout (%v) exceeded waiting for network to reach height %d. Got to height %d", t, h, latestHeight)
		case <-ticker.C:
			latestHeight, err = c.LatestHeight()
			if err != nil {
				return 0, err
			}
			if latestHeight >= h {
				return latestHeight, nil
			}
		}
	}
}

// WaitForTimestampWithTimeout waits for a block with a timestamp greater than t.
func (c *Context) WaitForTimestampWithTimeout(t time.Time, d time.Duration) (time.Time, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(c.goContext, d)
	defer cancel()

	var latestTimestamp time.Time
	for {
		select {
		case <-ctx.Done():
			return latestTimestamp, fmt.Errorf("timeout %v exceeded waiting for network to reach block with timestamp %v", d, t)
		case <-ticker.C:
			latestTimestamp, err := c.LatestTimestamp()
			if err != nil {
				return time.Unix(0, 0), err
			}
			if latestTimestamp.After(t) {
				return latestTimestamp, nil
			}
		}
	}
}

// WaitForHeight performs a blocking check where it waits for a block to be
// committed after a given block. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest height queried is returned.
func (c *Context) WaitForHeight(h int64) (int64, error) {
	return c.WaitForHeightWithTimeout(h, DefaultTimeout)
}

// WaitForTimestamp performs a blocking check where it waits for a block to be
// committed after a given timestamp. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest timestamp queried is returned.
func (c *Context) WaitForTimestamp(t time.Time) (time.Time, error) {
	return c.WaitForTimestampWithTimeout(t, 10*time.Second)
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func (c *Context) WaitForNextBlock() error {
	return c.WaitForBlocks(1)
}

// WaitForBlocks waits until n blocks have been committed, returning an error
// upon failure.
func (c *Context) WaitForBlocks(n int64) error {
	lastBlock, err := c.LatestHeight()
	if err != nil {
		return err
	}

	_, err = c.WaitForHeight(lastBlock + n)
	if err != nil {
		return err
	}

	return err
}

func (c *Context) WaitForTx(hashHexStr string, blocks int) (*rpctypes.ResultTx, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	height, err := c.LatestHeight()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(c.goContext, DefaultTimeout)
	defer cancel()

	for {
		latestHeight, err := c.LatestHeight()
		if err != nil {
			return nil, err
		}
		if blocks > 0 && latestHeight > height+int64(blocks) {
			return nil, fmt.Errorf("waited %d blocks for tx to be included in block", blocks)
		}

		<-ticker.C
		res, err := c.Client.Tx(ctx, hash, false)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			return nil, err
		}
		return res, nil
	}
}

// PostData will create and submit PFB transaction containing the namespace and
// blobData. This function blocks until the PFB has been included in a block and
// returns an error if the transaction is invalid or is rejected by the mempool.
func (c *Context) PostData(account, broadcastMode string, ns share.Namespace, blobData []byte) (*sdk.TxResponse, error) {
	rec, err := c.Keyring.Key(account)
	if err != nil {
		return nil, err
	}

	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	acc, seq, err := c.AccountRetriever.GetAccountNumberSequence(c.Context, addr)
	if err != nil {
		return nil, err
	}

	// use the key for accounts[i] to create a singer used for a single PFB
	signer, err := user.NewSigner(c.Keyring, c.TxConfig, c.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc, seq))
	if err != nil {
		return nil, err
	}

	b, err := types.NewV0Blob(ns, blobData)
	if err != nil {
		return nil, err
	}

	gas := types.DefaultEstimateGas([]uint32{uint32(len(blobData))})
	opts := blobfactory.FeeTxOpts(gas)

	blobTx, _, err := signer.CreatePayForBlobs(account, []*share.Blob{b}, opts...)
	if err != nil {
		return nil, err
	}

	// TODO: the signer is also capable of submitting the transaction via gRPC
	// so this section could eventually be replaced
	var res *sdk.TxResponse
	switch broadcastMode {
	case flags.BroadcastSync:
		res, err = c.BroadcastTxSync(blobTx)
	case flags.BroadcastAsync:
		res, err = c.BroadcastTxAsync(blobTx)
	case flags.BroadcastBlock:
		res, err = c.BroadcastTxCommit(blobTx)
	default:
		return nil, fmt.Errorf("unsupported broadcast mode %s; supported modes: sync, async, block", c.BroadcastMode)
	}
	if err != nil {
		return nil, err
	}
	if res.Code != abci.CodeTypeOK {
		return res, fmt.Errorf("failure to broadcast tx (%d): %s", res.Code, res.RawLog)
	}

	return res, nil
}

// FillBlock creates and submits a single transaction that is large enough to
// create a square of the desired size. broadcast mode indicates if the tx
// should be submitted async, sync, or block. (see flags.BroadcastModeSync). If
// broadcast mode is the string zero value, then it will be set to block.
func (c *Context) FillBlock(squareSize int, account string, broadcastMode string) (*sdk.TxResponse, error) {
	if squareSize < appconsts.MinSquareSize+1 || (squareSize&(squareSize-1) != 0) {
		return nil, fmt.Errorf("unsupported squareSize: %d", squareSize)
	}

	if broadcastMode == "" {
		broadcastMode = flags.BroadcastBlock
	}

	// create the tx the size of the square minus one row
	shareCount := (squareSize - 1) * squareSize

	// we use a formula to guarantee that the tx is the exact size needed to force a specific square size.
	blobSize := share.AvailableBytesFromSparseShares(shareCount)
	return c.PostData(account, broadcastMode, share.RandomBlobNamespace(), tmrand.Bytes(blobSize))
}

// HeightForTimestamp returns the block height for the first block after a
// given timestamp.
func (c *Context) HeightForTimestamp(timestamp time.Time) (int64, error) {
	latestHeight, err := c.LatestHeight()
	if err != nil {
		return 0, err
	}

	for i := int64(1); i <= latestHeight; i++ {
		result, err := c.Client.Block(context.Background(), &i)
		if err != nil {
			return 0, err
		}
		if result.Block.Time.After(timestamp) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("could not find block with timestamp after %v", timestamp)
}

func (c *Context) APIAddress() string {
	return c.apiAddress
}
