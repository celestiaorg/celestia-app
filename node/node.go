package node

import (
	"context"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/rsmt2d"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/types"
)

type PublishFn func(context.Context, *types.Header, *types.Commit, *types.ValidatorSet, *rsmt2d.ExtendedDataSquare) error

type Node struct {
	appConfig       *serverconfig.Config
	consensusConfig *tmconfig.Config
}

func Init(dir string) error {
	cmd := genutilcli.InitCmd(app.ModuleBasics, dir)
	cmd.SetArgs([]string{"node"})
	return cmd.Execute()
}

func Load(dir string) *Node {
	return &Node{}
}

func (n *Node) Run(ctx context.Context, publishFn PublishFn) error {
	return nil
}
