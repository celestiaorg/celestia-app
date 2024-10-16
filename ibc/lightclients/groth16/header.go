package groth16

import (
	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
)

var _ exported.Header = &Header{}

// ClientType defines that the Header is a Tendermint consensus algorithm
func (h Header) ClientType() string {
	return Groth16ClientType
}

// GetHeight returns the current height. It returns 0 if the tendermint
// header is nil.
// NOTE: the header.Header is checked to be non nil in ValidateBasic.
func (h Header) GetHeight() exported.Height {
	return clienttypes.NewHeight(0, uint64(h.NewHeight))
}

func (h Header) ValidateBasic() error {
	return nil
}

func (h Header) GetTrustedHeight() exported.Height {
	return clienttypes.NewHeight(0, uint64(h.TrustedHeight))
}
