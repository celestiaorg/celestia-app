package malicious

import (
	"bytes"
	"errors"
	"hash"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
)

// NOTE: This file is a copy of the original nmt.Hasher implementation, but with
// all of the validation logic removed. This allows for malicious nodes to create nmt roots that are out of order.

const (
	LeafPrefix = 0
	NodePrefix = 1
)

var _ hash.Hash = (*Hasher)(nil)

var (
	ErrInvalidNodeLen = errors.New("invalid NMT node size")
	ErrInvalidLeafLen = errors.New("invalid NMT leaf size")
)

type Hasher struct {
	baseHasher   hash.Hash
	NamespaceLen namespace.IDSize

	// The "ignoreMaxNs" flag influences the calculation of the namespace ID
	// range for intermediate nodes in the tree i.e., HashNode method. This flag
	// signals that, when determining the upper limit of the namespace ID range
	// for a tree node, the maximum possible namespace ID (equivalent to
	// "NamespaceLen" bytes of 0xFF, or 2^NamespaceLen-1) should be omitted if
	// feasible. For a more in-depth understanding of this field, refer to the
	// "HashNode".
	ignoreMaxNs      bool
	precomputedMaxNs namespace.ID

	tp   byte   // keeps type of NMT node to be hashed
	data []byte // written data of the NMT node
}

func (n *Hasher) IsMaxNamespaceIDIgnored() bool {
	return n.ignoreMaxNs
}

func (n *Hasher) NamespaceSize() namespace.IDSize {
	return n.NamespaceLen
}

// NewBlindHasher returns a Hasher that performs no ordering validation when the Write method is invoked.
func NewBlindHasher(baseHasher hash.Hash, nidLen namespace.IDSize, ignoreMaxNamespace bool) *Hasher {
	return &Hasher{
		baseHasher:       baseHasher,
		NamespaceLen:     nidLen,
		ignoreMaxNs:      ignoreMaxNamespace,
		precomputedMaxNs: bytes.Repeat([]byte{0xFF}, int(nidLen)),
	}
}

// Size returns the number of bytes Sum will return.
func (n *Hasher) Size() int {
	return n.baseHasher.Size() + int(n.NamespaceLen)*2
}

// Write writes the namespaced data to be hashed.
//
// Requires data of fixed size to match leaf or inner NMT nodes. Only a single
// write is allowed.
// It panics if more than one single write is attempted.
// If the data does not match the format of an NMT non-leaf node or leaf node, an error will be returned.
func (n *Hasher) Write(data []byte) (int, error) {
	if n.data != nil {
		panic("only a single Write is allowed")
	}

	ln := len(data)
	switch ln {
	// inner nodes are made up of the nmt hashes of the left and right children
	case n.Size() * 2:
		n.tp = NodePrefix
	// leaf nodes contain the namespace length and a share
	default:
		n.tp = LeafPrefix
	}

	n.data = data
	return ln, nil
}

// Sum computes the hash. Does not append the given suffix, violating the
// interface.
// It may panic if the data being hashed is invalid. This should never happen since the Write method refuses an invalid data and errors out.
func (n *Hasher) Sum([]byte) []byte {
	switch n.tp {
	case LeafPrefix:
		res, err := n.HashLeaf(n.data)
		if err != nil {
			panic(err) // this should never happen since the data is already validated in the Write method
		}
		return res
	case NodePrefix:
		flagLen := int(n.NamespaceLen) * 2
		sha256Len := n.baseHasher.Size()
		leftChild := n.data[:flagLen+sha256Len]
		rightChild := n.data[flagLen+sha256Len:]
		res, err := n.HashNode(leftChild, rightChild)
		if err != nil {
			panic(err) // this should never happen since the data is already validated in the Write method
		}
		return res
	default:
		panic("nmt node type wasn't set")
	}
}

// Reset resets the Hash to its initial state.
func (n *Hasher) Reset() {
	n.tp, n.data = 255, nil // reset with an invalid node type, as zero value is a valid Leaf
	n.baseHasher.Reset()
}

// BlockSize returns the hash's underlying block size.
func (n *Hasher) BlockSize() int {
	return n.baseHasher.BlockSize()
}

func (n *Hasher) EmptyRoot() []byte {
	n.baseHasher.Reset()
	emptyNs := bytes.Repeat([]byte{0}, int(n.NamespaceLen))
	h := n.baseHasher.Sum(nil)
	digest := append(append(emptyNs, emptyNs...), h...)

	return digest
}

// HashLeaf computes namespace hash of the namespaced data item `ndata` as
// ns(ndata) || ns(ndata) || hash(leafPrefix || ndata), where ns(ndata) is the
// namespaceID inside the data item namely leaf[:n.NamespaceLen]). Note that for
// leaves minNs = maxNs = ns(leaf) = leaf[:NamespaceLen]. HashLeaf can return the ErrInvalidNodeLen error if the input is not namespaced.
//
//nolint:errcheck
func (n *Hasher) HashLeaf(ndata []byte) ([]byte, error) {
	h := n.baseHasher
	h.Reset()

	nID := ndata[:n.NamespaceLen]
	resLen := int(2*n.NamespaceLen) + n.baseHasher.Size()
	minMaxNIDs := make([]byte, 0, resLen)
	minMaxNIDs = append(minMaxNIDs, nID...) // nID
	minMaxNIDs = append(minMaxNIDs, nID...) // nID || nID

	// add LeafPrefix to the ndata
	leafPrefixedNData := make([]byte, 0, len(ndata)+1)
	leafPrefixedNData = append(leafPrefixedNData, LeafPrefix)
	leafPrefixedNData = append(leafPrefixedNData, ndata...)
	h.Write(leafPrefixedNData)

	// compute h(LeafPrefix || ndata) and append it to the minMaxNIDs
	nameSpacedHash := h.Sum(minMaxNIDs) // nID || nID || h(LeafPrefix || ndata)
	return nameSpacedHash, nil
}

// MustHashLeaf is a wrapper around HashLeaf that panics if an error is
// encountered. The ndata must be a valid leaf node.
func (n *Hasher) MustHashLeaf(ndata []byte) []byte {
	res, err := n.HashLeaf(ndata)
	if err != nil {
		panic(err)
	}
	return res
}

// HashNode calculates a namespaced hash of a node using the supplied left and
// right children. The input values, `left` and `right,` are namespaced hash
// values with the format `minNID || maxNID || hash.`
// The HashNode function returns an error if the provided inputs are invalid. Specifically, it returns the ErrInvalidNodeLen error if the left and right inputs are not in the namespaced hash format,
// and the ErrUnorderedSiblings error if left.maxNID is greater than right.minNID.
// By default, the normal namespace hash calculation is
// followed, which is `res = min(left.minNID, right.minNID) || max(left.maxNID,
// right.maxNID) || H(NodePrefix, left, right)`. `res` refers to the return
// value of the HashNode. However, if the `ignoreMaxNs` property of the Hasher
// is set to true, the calculation of the namespace ID range of the node
// slightly changes. Let MAXNID be the maximum possible namespace ID value i.e., 2^NamespaceIDSize-1.
// If the namespace range of the right child is start=end=MAXNID, indicating that it represents the root of a subtree whose leaves all have the namespace ID of `MAXNID`, then exclude the right child from the namespace range calculation. Instead,
// assign the namespace range of the left child as the parent's namespace range.
func (n *Hasher) HashNode(left, right []byte) ([]byte, error) {
	h := n.baseHasher
	h.Reset()

	leftMinNs, leftMaxNs := nmt.MinNamespace(left, n.NamespaceLen), nmt.MaxNamespace(left, n.NamespaceLen)
	rightMinNs, rightMaxNs := nmt.MinNamespace(right, n.NamespaceLen), nmt.MaxNamespace(right, n.NamespaceLen)

	// compute the namespace range of the parent node
	minNs, maxNs := computeNsRange(leftMinNs, leftMaxNs, rightMinNs, rightMaxNs, n.ignoreMaxNs, n.precomputedMaxNs)

	res := make([]byte, 0)
	res = append(res, minNs...)
	res = append(res, maxNs...)

	// Note this seems a little faster than calling several Write()s on the
	// underlying Hash function (see:
	// https://github.com/google/trillian/pull/1503):
	data := make([]byte, 0, 1+len(left)+len(right))
	data = append(data, NodePrefix)
	data = append(data, left...)
	data = append(data, right...)
	//nolint:errcheck
	h.Write(data)
	return h.Sum(res), nil
}

// computeNsRange computes the namespace range of the parent node based on the namespace ranges of its left and right children.
func computeNsRange(leftMinNs, leftMaxNs, rightMinNs, rightMaxNs []byte, ignoreMaxNs bool, precomputedMaxNs namespace.ID) (minNs []byte, maxNs []byte) {
	minNs = leftMinNs
	maxNs = rightMaxNs
	if ignoreMaxNs && bytes.Equal(precomputedMaxNs, rightMinNs) {
		maxNs = leftMaxNs
	}
	return minNs, maxNs
}
