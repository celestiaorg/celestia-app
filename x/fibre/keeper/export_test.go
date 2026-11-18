package keeper

import "github.com/celestiaorg/celestia-app/v11/x/fibre/types"

// DecodePayForFibreWithoutCertificates exposes FinalizeBlock's decode to tests.
func DecodePayForFibreWithoutCertificates(raw []byte) *types.DecodedPayForFibre {
	return decodePayForFibre(raw, false)
}
