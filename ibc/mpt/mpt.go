package mpt

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func VerifyMerklePatriciaTrieProof(stateRoot []byte, key string, proof []byte) (value []byte, err error) {
	rootHash := common.BytesToHash(stateRoot)
	keyBytes := common.FromHex(key)
	proofDB := rawdb.NewMemoryDatabase()
	err = rlp.DecodeBytes(proof, proofDB)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proof: %w", err)
	}
	return trie.VerifyProof(rootHash, keyBytes, proofDB)
}
