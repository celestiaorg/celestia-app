package malicious

import (
	"bytes"
	"errors"

	abci "github.com/cometbft/cometbft/abci/types"
)

// BadDataRootPrepareProposal builds an honest proposal and then flips one bit
// of its data root, so every proposal it makes is invalid, with or without
// blobs.
func (a *App) BadDataRootPrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	resp, err := a.App.PrepareProposal(req)
	if err != nil {
		return nil, err
	}
	if len(resp.DataRootHash) == 0 {
		return nil, errors.New("honest proposal has an empty data root")
	}
	resp.DataRootHash = bytes.Clone(resp.DataRootHash)
	resp.DataRootHash[0] ^= 0x01
	return resp, nil
}
