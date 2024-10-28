package mpt_test

import (
	"bytes"
	mrand "math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/ibc/mpt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	gethtrie "github.com/ethereum/go-ethereum/trie"
	"github.com/stretchr/testify/require"

	crand "crypto/rand"
	"encoding/binary"
	"encoding/gob"
	"fmt"
)

type kv struct {
	k, v []byte
	t    bool
}

var prng = initRnd()

func TestVerifyMerklePatriciaTrieProof(t *testing.T) {
	trie, vals := randomTrie(500)
	root := trie.Hash()
	for i, prover := range makeProvers(trie) {

		for _, kv := range vals {
			proof := prover(kv.k)
			proofBytes, err := proofListToBytes(*proof)
			require.NoError(t, err)

			if proof == nil {
				t.Fatalf("prover %d: missing key %x while constructing proof", i, kv.k)
			}

			val, err := mpt.VerifyMerklePatriciaTrieProof(root.Bytes(), string(kv.k), proofBytes)
			if err != nil {
				t.Fatalf("prover %d: failed to verify proof for key %x: %v\nraw proof: %x", i, kv.k, err, proof)
			}
			if !bytes.Equal(val, kv.v) {
				t.Fatalf("prover %d: verified value mismatch for key %x: have %x, want %x", i, kv.k, val, kv.v)
			}
		}
	}
}

func randomTrie(n int) (trie *gethtrie.Trie, vals map[string]*kv) {
	trie = gethtrie.NewEmpty(newTestDatabase(rawdb.NewMemoryDatabase(), rawdb.HashScheme))
	vals = make(map[string]*kv)
	for i := byte(0); i < 100; i++ {
		value := &kv{common.LeftPadBytes([]byte{i}, 32), []byte{i}, false}
		value2 := &kv{common.LeftPadBytes([]byte{i + 10}, 32), []byte{i}, false}
		trie.MustUpdate(value.k, value.v)
		trie.MustUpdate(value2.k, value2.v)
		vals[string(value.k)] = value
		vals[string(value2.k)] = value2
	}
	for i := 0; i < n; i++ {
		value := &kv{randBytes(32), randBytes(20), false}
		trie.MustUpdate(value.k, value.v)
		vals[string(value.k)] = value
	}
	return trie, vals
}

func randBytes(n int) []byte {
	r := make([]byte, n)
	prng.Read(r)
	return r
}

func initRnd() *mrand.Rand {
	var seed [8]byte
	crand.Read(seed[:])
	rnd := mrand.New(mrand.NewSource(int64(binary.LittleEndian.Uint64(seed[:]))))
	fmt.Printf("Seed: %x\n", seed)
	return rnd
}

// makeProvers creates Merkle trie provers based on different implementations to
// test all variations.
func makeProvers(trie *gethtrie.Trie) []func(key []byte) *mpt.ProofList {
	var provers []func(key []byte) *mpt.ProofList

	// Create a direct trie based Merkle prover
	provers = append(provers, func(key []byte) *mpt.ProofList {
		var proof mpt.ProofList
		trie.Prove(key, &proof)
		return &proof
	})
	// Create a leaf iterator based Merkle prover
	provers = append(provers, func(key []byte) *mpt.ProofList {
		var proof mpt.ProofList
		if it := gethtrie.NewIterator(trie.MustNodeIterator(key)); it.Next() && bytes.Equal(key, it.Key) {
			for _, p := range it.Prove() {
				proof.Put(crypto.Keccak256(p), p)
			}
		}
		return &proof
	})
	return provers
}

// Converts proofList to []byte by encoding it with gob
func proofListToBytes(proof mpt.ProofList) ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(proof); err != nil {
		return nil, fmt.Errorf("failed to encode proofList to bytes: %w", err)
	}
	return buf.Bytes(), nil
}
