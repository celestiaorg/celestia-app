package node

import (
	"context"

	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/types"
)

type PublishFn func(context.Context, *types.Header, *types.Commit, *types.ValidatorSet, *rsmt2d.ExtendedDataSquare) error

type Node struct {
	
}

func Init(dir string) *Node {
	return &Node{}
}

func Load(dir string) *Node {
	return &Node{}
}

func (n *Node) Run(ctx context.Context, publishFn PublishFn) error {
	return nil
}

func (n *Node) Save() error