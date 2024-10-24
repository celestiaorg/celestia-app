package mpt_test

import (
	"bytes"
	mrand "math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/ibc/mpt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	gethtrie "github.com/ethereum/go-ethereum/trie"

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
			proofBytes, err := serializeProofDB(proof)
			if err != nil {
				t.Fatalf("prover %d: failed to serialize proof for key %x: %v", i, kv.k, err)
			}

			if proof == nil {
				t.Fatalf("prover %d: missing key %x while constructing proof", i, kv.k)
			}

			val, err := mpt.VerifyMerklePatriciaTrieProof(root.Bytes(), string(kv.k), proofBytes)
			fmt.Println(val, "VALUES")
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
func makeProvers(trie *gethtrie.Trie) []func(key []byte) *memorydb.Database {
	var provers []func(key []byte) *memorydb.Database

	// Create a direct trie based Merkle prover
	provers = append(provers, func(key []byte) *memorydb.Database {
		proof := memorydb.New()
		trie.Prove(key, proof)
		return proof
	})
	// Create a leaf iterator based Merkle prover
	provers = append(provers, func(key []byte) *memorydb.Database {
		proof := memorydb.New()
		if it := gethtrie.NewIterator(trie.MustNodeIterator(key)); it.Next() && bytes.Equal(key, it.Key) {
			for _, p := range it.Prove() {
				proof.Put(crypto.Keccak256(p), p)
			}
		}
		return proof
	})
	return provers
}

func serializeProofDB(proof *memorydb.Database) ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	it := proof.NewIterator(nil, nil)
	for it.Next() {
		key := it.Key()
		value := it.Value()

		// Encode the key and value pair
		if err := encoder.Encode(key); err != nil {
			return nil, err
		}
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
	}

	// Return the serialized byte slice
	return buf.Bytes(), nil
}
