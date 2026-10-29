package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// rootHistory is a per-ISM bounded FIFO of replaced roots, keyed by (ism id, sequence).
type rootHistory = collections.Map[collections.Pair[uint64, uint64], types.RootHistoryEntry]

// pushHistory records a replaced root for an ISM and trims the history to size entries.
func pushHistory(ctx context.Context, history rootHistory, ismID uint64, size uint32, entry types.RootHistoryEntry) error {
	newestSeq, found, err := newestSequence(ctx, history, ismID)
	if err != nil {
		return err
	}

	seq := uint64(0)
	if found {
		seq = newestSeq + 1
	}

	entry.Height = sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := history.Set(ctx, collections.Join(ismID, seq), entry); err != nil {
		return err
	}

	return trimHistory(ctx, history, ismID, seq, size)
}

// trimHistory removes the entries of an ISM that fall outside the newest size sequences.
func trimHistory(ctx context.Context, history rootHistory, ismID, newestSeq uint64, size uint32) error {
	if newestSeq < uint64(size) {
		return nil
	}

	cutoff := newestSeq + 1 - uint64(size)
	return history.Clear(ctx, collections.NewPrefixedPairRange[uint64, uint64](ismID).EndExclusive(cutoff))
}

// resizeHistory trims the history of an ISM to a new root history size.
func resizeHistory(ctx context.Context, history rootHistory, ismID uint64, size uint32) error {
	newestSeq, found, err := newestSequence(ctx, history, ismID)
	if err != nil || !found {
		return err
	}

	return trimHistory(ctx, history, ismID, newestSeq, size)
}

// clearHistory removes every history entry of an ISM.
func clearHistory(ctx context.Context, history rootHistory, ismID uint64) error {
	return history.Clear(ctx, collections.NewPrefixedPairRange[uint64, uint64](ismID))
}

// newestSequence returns the highest sequence in the history of an ISM.
func newestSequence(ctx context.Context, history rootHistory, ismID uint64) (uint64, bool, error) {
	iter, err := history.Iterate(ctx, collections.NewPrefixedPairRange[uint64, uint64](ismID).Descending())
	if err != nil {
		return 0, false, err
	}
	defer iter.Close()

	if !iter.Valid() {
		return 0, false, nil
	}

	key, err := iter.Key()
	if err != nil {
		return 0, false, err
	}

	return key.K2(), true, nil
}

// historyContains reports whether any history entry of an ISM satisfies match, checking the newest entries first.
func historyContains(ctx context.Context, history rootHistory, ismID uint64, match func(types.RootHistoryEntry) bool) (bool, error) {
	iter, err := history.Iterate(ctx, collections.NewPrefixedPairRange[uint64, uint64](ismID).Descending())
	if err != nil {
		return false, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		entry, err := iter.Value()
		if err != nil {
			return false, err
		}
		if match(entry) {
			return true, nil
		}
	}

	return false, nil
}

// historyEntries returns the history entries of an ISM, oldest first.
func historyEntries(ctx context.Context, history rootHistory, ismID uint64) ([]types.RootHistoryEntry, error) {
	iter, err := history.Iterate(ctx, collections.NewPrefixedPairRange[uint64, uint64](ismID))
	if err != nil {
		return nil, err
	}

	return iter.Values()
}
