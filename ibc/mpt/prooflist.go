package mpt

import "github.com/ethereum/go-ethereum/common/hexutil"

// proofList implements ethdb.KeyValueWriter and collects the proofs as
// hex-strings for delivery to rpc-caller.
type ProofList []string

func (n *ProofList) Put(key []byte, value []byte) error {
	*n = append(*n, hexutil.Encode(value))
	return nil
}

func (n *ProofList) Delete(key []byte) error {
	panic("not supported")
}
