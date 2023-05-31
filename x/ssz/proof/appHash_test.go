package proof

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/tmhash"
	cmtrand "github.com/tendermint/tendermint/libs/rand"
	cmtversion "github.com/tendermint/tendermint/proto/tendermint/version"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

// Copied from types/block_test.go
func makeRandHeader() types.Header {
	chainID := "test"
	t := time.Now()
	height := cmtrand.Int63()
	randBytes := cmtrand.Bytes(tmhash.Size)
	randAddress := cmtrand.Bytes(crypto.AddressSize)
	h := types.Header{
		Version:            cmtversion.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID:            chainID,
		Height:             height,
		Time:               t,
		LastBlockID:        types.BlockID{},
		LastCommitHash:     randBytes,
		DataHash:           randBytes,
		ValidatorsHash:     randBytes,
		NextValidatorsHash: randBytes,
		ConsensusHash:      randBytes,
		AppHash:            randBytes,

		LastResultsHash: randBytes,

		EvidenceHash:    randBytes,
		ProposerAddress: randAddress,
	}

	return h
}

func TestAppHashEncode(t *testing.T) {
	h := makeRandHeader()
	encoded := cdcEncode(h.AppHash)
	appHashBytes := h.AppHash.Bytes()
	computedEncoding := EncodeAppHash(appHashBytes)
	require.Equal(t, encoded, computedEncoding, "Encoded app hash should be equal to the computed encoding")
}

func TestAppHashProof(t *testing.T) {
	h := makeRandHeader()
	rootHash, appHash, proof := GenerateAppHashProof(&h)

	verified := VerifyAppHashProof(rootHash, appHash, proof)
	require.True(t, verified, "App hash proof should be verified")
}
