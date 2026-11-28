package hdrchain

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
)

// Params returns the consensus params in effect at height h (/consensus_params).
func (s *HTTPSource) Params(ctx context.Context, h int64) (types.ConsensusParams, error) {
	raw, err := s.get(ctx, "consensus_params", url.Values{"height": {strconv.FormatInt(h, 10)}})
	if err != nil {
		return types.ConsensusParams{}, err
	}
	res, err := unwrap(raw)
	if err != nil {
		return types.ConsensusParams{}, err
	}
	var rc coretypes.ResultConsensusParams
	if err := cmtjson.Unmarshal(res, &rc); err != nil {
		return types.ConsensusParams{}, fmt.Errorf("decode /consensus_params result: %w", err)
	}
	if rc.BlockHeight != h {
		return types.ConsensusParams{}, fmt.Errorf("consensus params for height %d, asked for %d", rc.BlockHeight, h)
	}
	return rc.ConsensusParams, nil
}

// Params returns the consensus params in effect at height h from state.db.
func (s *BlockstoreSource) Params(_ context.Context, h int64) (types.ConsensusParams, error) {
	if s.State == nil {
		return types.ConsensusParams{}, errors.New("blockstore source has no state store for consensus params")
	}
	return guard("state store consensus params", h, func() (types.ConsensusParams, error) { return s.State.LoadConsensusParams(h) })
}
