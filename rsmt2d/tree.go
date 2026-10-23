package rsmt2d

import (
	"crypto/sha256"

	"github.com/celestiaorg/merkletree"
)

// TreeConstructorFn creates a fresh Tree instance to be used as the Merkle tree
// inside of rsmt2d.
type TreeConstructorFn = func(axis Axis, index uint) Tree

type BufferedTreeConstructor interface {
	NewConstructor(squareSize uint) TreeConstructorFn
	TreeCount() int
}

// SquareIndex contains all information needed to identify the cell that is being
// pushed
type SquareIndex struct {
	Axis, Cell uint
}

// Tree wraps Merkle tree implementations to work with rsmt2d
type Tree interface {
	Push(data []byte) error
	Root() ([]byte, error)
}

var _ Tree = &DefaultTree{}

type DefaultTree struct {
	*merkletree.Tree
	leaves [][]byte
	root   []byte
}

func NewDefaultTree(_ Axis, _ uint) Tree {
	return &DefaultTree{
		Tree:   merkletree.New(sha256.New()),
		leaves: make([][]byte, 0, 128),
	}
}

func (d *DefaultTree) Push(data []byte) error {
	// ignore the idx, as this implementation doesn't need that info
	d.leaves = append(d.leaves, data)
	return nil
}

func (d *DefaultTree) Root() ([]byte, error) {
	if d.root == nil {
		for _, l := range d.leaves {
			d.Tree.Push(l)
		}
		d.root = d.Tree.Root()
	}
	return d.root, nil
}
