package internal

import (
	"encoding/json"
	"errors"
	"os"

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
	ConsensusParams struct {
		Version struct {
			AppVersion string `json:"app_version"`
		} `json:"version"`
	} `json:"consensus_params"`
}

type genesisDocv2 struct {
	Consensus struct {
		Params struct {
			Version struct {
				App string `json:"app"`
			} `json:"version"`
		} `json:"params"`
	} `json:"consensus"`
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
		if v1.ConsensusParams.Version.AppVersion != "" {
			return GenesisVersion1, nil
		}
	}

	var v2 genesisDocv2
	if err := json.Unmarshal(genDoc, &v2); err == nil {
		if v2.Consensus.Params.Version.App != "" {
			return GenesisVersion2, nil
		}
	}

	return 0, errors.New("failed to determine genesis version")
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
