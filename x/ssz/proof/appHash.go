package proof

import (
	"bytes"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/types"
)

const (
	AppHashIndexInHeader = 10
)

// Taken from tendermint/tendermint/crypto/merkle/proof.go
var (
	leafPrefix  = []byte{0}
	innerPrefix = []byte{1}
)

func GenerateAppHashProof(h *types.Header) ([]byte, []byte, [][]byte) {
	hbz, err := h.Version.Marshal()
	if err != nil {
		panic("could not marshal version")
	}

	pbt, err := gogotypes.StdTimeMarshal(h.Time)
	if err != nil {
		panic("could not marshal time")
	}

	pbbi := h.LastBlockID.ToProto()
	bzbi, err := pbbi.Marshal()

	rootHash, proofs := merkle.ProofsFromByteSlices([][]byte{
		hbz,
		cdcEncode(h.ChainID),
		cdcEncode(h.Height),
		pbt,
		bzbi,
		cdcEncode(h.LastCommitHash),
		cdcEncode(h.DataHash),
		cdcEncode(h.ValidatorsHash),
		cdcEncode(h.NextValidatorsHash),
		cdcEncode(h.ConsensusHash),
		cdcEncode(h.AppHash),
		cdcEncode(h.LastResultsHash),
		cdcEncode(h.EvidenceHash),
		cdcEncode(h.ProposerAddress),
	})
	return rootHash, h.AppHash.Bytes(), proofs[AppHashIndexInHeader].Aunts
}

func VerifyAppHashProof(rootHash []byte, appHash []byte, proof [][]byte) bool {
	leafHash := Hash(append(leafPrefix, EncodeAppHash(appHash)...))
	computedRoot := leafHash
	idx := AppHashIndexInHeader
	for _, aunt := range proof {
		res := append(computedRoot, aunt...)
		if idx%2 == 1 {
			res = append(aunt, computedRoot...)
		}
		computedRoot = Hash(append(innerPrefix, res...))
		idx /= 2
	}
	return bytes.Equal(rootHash, computedRoot)
}

func EncodeAppHash(appHash []byte) []byte {
	return append([]byte{0xa, 0x20}, appHash...)
}
