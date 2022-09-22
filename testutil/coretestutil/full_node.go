package coretestutil

import (
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/tendermint/tendermint/config"
	tmjson "github.com/tendermint/tendermint/libs/json"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

func NewTendermintNode(t *testing.T, cfg *config.Config, supressLog bool) (*node.Node, error) {
	var logger log.Logger
	if supressLog {
		logger = log.NewNopLogger()
	} else {
		logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
		logger = log.NewFilter(logger, log.AllowError())
	}

	cfg.Genesis = "./testdata/genesis.json"
	cfg.RootDir = t.TempDir()

	pv := loadPV()

	// papp := proxy.NewLocalClientCreator(app)
	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		panic(err)
	}

	db := dbm.NewMemDB()

	return node.NewNode(
		cfg,
		pv,
		nodeKey,
		proxy.NewLocalClientCreator(cmd.NewAppServer(logger, db, nil, emptyAppOptions{})),
		localGenesisCreator,
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.Instrumentation),
		logger,
	)
}

func loadPV() *privval.FilePV {
	pvKey := privval.FilePVKey{}
	err := tmjson.Unmarshal([]byte(privVal), &pvKey)
	if err != nil {
		panic("Error reading PrivValidator key from")
	}

	// overwrite pubkey and address for convenience
	pvKey.PubKey = pvKey.PrivKey.PubKey()
	pvKey.Address = pvKey.PubKey.Address()

	pvState := privval.FilePVLastSignState{}

	return &privval.FilePV{
		Key:           pvKey,
		LastSignState: pvState,
	}
}

func localGenesisCreator() (*types.GenesisDoc, error) {
	return types.GenesisDocFromJSON([]byte(genesis))
}

type emptyAppOptions struct{}

// Get implements AppOptions
func (ao emptyAppOptions) Get(o string) interface{} {
	return nil
}
