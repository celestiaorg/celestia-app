package mpt

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
)

func VerifyMerklePatriciaTrieProof(stateRoot []byte, key string, proof []byte) (value []byte, err error) {
	rootHash := common.BytesToHash(stateRoot)
	proodDB, err := deserializeProofDB(proof)

	if err != nil {
		return nil, fmt.Errorf("failed to decode proof: %w", err)
	}
	return trie.VerifyProof(rootHash, []byte(key), proodDB)
}

func deserializeProofDB(proof []byte) (ethdb.Database, error) {
	buf := bytes.NewBuffer(proof)
	decoder := gob.NewDecoder(buf)
	proofDB := rawdb.NewMemoryDatabase()

	for buf.Len() > 0 {
		var key, value []byte

		// Decode the key and value pair
		if err := decoder.Decode(&key); err != nil {
			return nil, err
		}
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}

		// Insert the key-value pair back into the proofDB
		proofDB.Put(key, value)
	}

	return proofDB, nil
}
