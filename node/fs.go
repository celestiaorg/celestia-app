package node

import (
	"os"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/app"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/viper"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

type Filesystem struct {
	App       *serverconfig.Config
	Consensus *tmconfig.Config
	genesis   *types.GenesisDoc
	signer    *privval.FilePV
	nodeKey   *p2p.NodeKey
}

func NewFilesystem(
	app *serverconfig.Config,
	consensus *tmconfig.Config,
	genesis *types.GenesisDoc,
	signer *privval.FilePV,
	nodeKey *p2p.NodeKey,
) *Filesystem {
	return &Filesystem{
		App:       app,
		Consensus: consensus,
		genesis:   genesis,
		signer:    signer,
		nodeKey:   nodeKey,
	}
}

func DefaultFilesystem(dir string) *Filesystem {
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(dir)
	pv := privval.GenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	return &Filesystem{
		App:       app.DefaultAppConfig(),
		Consensus: cfg,
		signer:    pv,
		nodeKey: &p2p.NodeKey{
			PrivKey: ed25519.GenPrivKey(),
		},
	}
}

func Load(dir string) (*Filesystem, error) {
	consensusCfg, err := LoadConsensusConfig(dir)
	if err != nil {
		return nil, err
	}
	appCfg, err := LoadAppConfig(dir)
	if err != nil {
		return nil, err
	}

	return NewFilesystem(appCfg, consensusCfg, nil, nil, nil), nil
}

func (fs *Filesystem) Save() error {
	// save the consensus config, overwriting the existing file
	tmconfig.WriteConfigFile(filepath.Join(fs.Consensus.RootDir, "config", "config.toml"), fs.Consensus)

	// save the application config, overwriting the existing file
	serverconfig.WriteConfigFile(filepath.Join(fs.Consensus.RootDir, "config", "app.toml"), fs.App)

	// if no genesis exists and one is provided, save it (it's not possible to override an
	// existing genesis file)
	genesisFile := filepath.Join(fs.Consensus.RootDir, "config", "genesis.json")
	if !fileExists(genesisFile) && fs.genesis != nil {
		if err := fs.genesis.SaveAs(genesisFile); err != nil {
			return err
		}
	}

	// save the node key
	if !fileExists(fs.Consensus.NodeKeyFile()) && fs.nodeKey != nil {
		if err := fs.nodeKey.SaveAs(fs.Consensus.NodeKeyFile()); err != nil {
			return err
		}
	}

	// save the consensus signer (filePV)
	if fs.signer != nil {
		fs.signer.Save()
	}

	// TODO: add addressbook.json
	return nil
}

func (fs *Filesystem) Genesis() (*types.GenesisDoc, error) {
	var err error
	if fs.genesis == nil {
		fs.genesis, err = types.GenesisDocFromFile(fs.Consensus.GenesisFile())
	}
	return fs.genesis, err
}

func LoadConsensusConfig(dir string) (*tmconfig.Config, error) {
	cfg := app.DefaultConsensusConfig()
	path := filepath.Join(dir, "config")
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}
	if err := cfg.ValidateBasic(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadAppConfig(dir string) (*serverconfig.Config, error) {
	cfg := app.DefaultAppConfig()
	path := filepath.Join(dir, "config")
	viper.AddConfigPath(path)
	viper.SetConfigName("app")
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}
	if err := cfg.ValidateBasic(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func TestGenesis() *types.GenesisDoc {
	return &types.GenesisDoc{}
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return os.IsExist(err)
}
