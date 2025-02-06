package simulation

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/kv"
)

// NewDecodeStore returns a decoder function closure that unmarshals the KVPair's
// Value to the corresponding mint type.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.Equal(kvA.Key, types.KeyMinter):
			var minterA, minterB types.Minter
			cdc.MustUnmarshal(kvA.Value, &minterA)
			cdc.MustUnmarshal(kvB.Value, &minterB)
			return fmt.Sprintf("%v\n%v", minterA, minterB)
		case bytes.Equal(kvA.Key, types.KeyGenesisTime):
			var genesisTimeA, genesisTimeB types.GenesisTime
			cdc.MustUnmarshal(kvA.Value, &genesisTimeA)
			cdc.MustUnmarshal(kvB.Value, &genesisTimeB)
			return fmt.Sprintf("%v\n%v", genesisTimeA, genesisTimeB)
		default:
			panic(fmt.Sprintf("invalid mint key %X", kvA.Key))
		}
	}
}
