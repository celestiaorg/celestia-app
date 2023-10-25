package node

import (
	"path/filepath"

	"github.com/celestiaorg/celestia-app/app"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/viper"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/types"
)

type Filesystem struct {
	App       *serverconfig.Config
	Consensus *tmconfig.Config
}

func NewFileSystem(app *serverconfig.Config, consensus *tmconfig.Config) *Filesystem {
	return &Filesystem{
		App:       app,
		Consensus: consensus,
	}
}

func Init(dir string) *Filesystem {
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(dir)
	return &Filesystem{
		App:       app.DefaultAppConfig(),
		Consensus: cfg,
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

	return NewFileSystem(appCfg, consensusCfg), nil
}

func (fs *Filesystem) Save() error {
	return nil
}

func (fs *Filesystem) Genesis() (*types.GenesisDoc, error) {
	return types.GenesisDocFromFile(fs.Consensus.GenesisFile())
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
