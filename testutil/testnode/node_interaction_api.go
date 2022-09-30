package testnode

import (
	"context"
	"errors"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
)

// LatestHeight returns the latest height of the network or an error if the
// query fails.
func LatestHeight(cctx client.Context) (int64, error) {
	status, err := cctx.Client.Status(context.Background())
	if err != nil {
		return 0, err
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func WaitForHeightWithTimeout(cctx client.Context, h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(t)
	defer timeout.Stop()

	var latestHeight int64
	for {
		select {
		case <-timeout.C:
			return latestHeight, errors.New("timeout exceeded waiting for block")
		case <-ticker.C:
			latestHeight, err := LatestHeight(cctx)
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
func WaitForHeight(cctx client.Context, h int64) (int64, error) {
	return WaitForHeightWithTimeout(cctx, h, 10*time.Second)
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func WaitForNextBlock(cctx client.Context) error {
	lastBlock, err := LatestHeight(cctx)
	if err != nil {
		return err
	}

	_, err = WaitForHeight(cctx, lastBlock+1)
	if err != nil {
		return err
	}

	return err
}
