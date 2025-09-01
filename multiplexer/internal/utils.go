package internal

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

type GenesisVersion int

const (
	GenesisVersion1 GenesisVersion = iota
	GenesisVersion2
)

type genesisDocv1 struct {
	ChainID         string `json:"chain_id"`
	ConsensusParams struct {
		Version struct {
			AppVersion string `json:"app_version"`
		} `json:"version"`
	} `json:"consensus_params"`
}

var ErrGenesisNotFound = errors.New("genesis not found")

// GetGenesisVersion returns the genesis version for the given genesis path.
func GetGenesisVersion(genesisPath string) (GenesisVersion, error) {
	genDoc, err := os.ReadFile(genesisPath)
	if err != nil {
		return 0, errors.Join(ErrGenesisNotFound, err)
	}

	var v1 genesisDocv1
	if err := json.Unmarshal(genDoc, &v1); err == nil {
		// The mocha genesis file contains an empty version field so we need to
		// special case it.
		if v1.ChainID == appconsts.MochaChainID {
			return GenesisVersion1, nil
		}
		if v1.ConsensusParams.Version.AppVersion != "" {
			return GenesisVersion1, nil
		}
	}

	return GenesisVersion2, nil
}

// GetGenDocProvider returns a function which returns the genesis doc from the genesis file.
func GetGenDocProvider(cfg *cmtcfg.Config) func() (*cmttypes.GenesisDoc, error) {
	return func() (*cmttypes.GenesisDoc, error) {
		appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
		if err != nil {
			return nil, err
		}

		return appGenesis.ToGenesisDoc()
	}
}
