package mpt

import (
	"bytes"
	"encoding/gob"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
)

func VerifyMerklePatriciaTrieProof(stateRoot []byte, key string, proof []byte) (value []byte, err error) {
	rootHash := common.BytesToHash(stateRoot)
	bytesToProofList, err := bytesToProofList(proof)
	proofDB, err := ReconstructProofDB(bytesToProofList)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proof: %w", err)
	}
	return trie.VerifyProof(rootHash, []byte(key), proofDB)
}

// bytesToProofList converts []byte back to proofList by decoding with gob
func bytesToProofList(data []byte) (ProofList, error) {
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	var proof ProofList
	if err := decoder.Decode(&proof); err != nil {
		return nil, fmt.Errorf("failed to decode bytes to proofList: %w", err)
	}
	return proof, nil
}

// ReconstructProofDB calculates the node hashes sets them as keys in the db and
// each decoded hexNode from the proof list as a value
func ReconstructProofDB(proof ProofList) (ethdb.Database, error) {
	// Initialize an in-memory database
	proofDB := rawdb.NewMemoryDatabase()

	// Decode each hex-encoded node in ProofList, hash it, and insert it into proofDB
	for _, hexNode := range proof {
		// Decode the hex-encoded node
		node, err := hexutil.Decode(hexNode)
		if err != nil {
			return nil, fmt.Errorf("failed to decode proof node: %w", err)
		}

		// Compute the hash of the node, which will serve as the key in proofDB
		nodeHash := crypto.Keccak256(node)

		// Insert the node into proofDB with its hash as the key
		if err := proofDB.Put(nodeHash, node); err != nil {
			return nil, fmt.Errorf("failed to insert proof node: %w", err)
		}
	}

	return proofDB, nil
}
