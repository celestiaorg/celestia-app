package genesis

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tendermint/tendermint/config"
	tmos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
)

// InitFiles initializes the files for a new tendermint node with the provided
// genesis. It will use the validatorIndex to save the validator's consensus
// key.
func InitFiles(
	dir string,
	tmCfg *config.Config,
	g *Genesis,
	validatorIndex int,
) (string, error) {
	val, has := g.Validator(validatorIndex)
	if !has {
		return "", fmt.Errorf("validator %d not found", validatorIndex)
	}

	basePath := filepath.Join(dir, ".celestia-app")
	tmCfg.SetRoot(basePath)

	// save the genesis file
	configPath := filepath.Join(basePath, "config")
	err := os.MkdirAll(configPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	gDoc, err := g.Export()
	if err != nil {
		return "", fmt.Errorf("exporting genesis: %w", err)
	}
	err = gDoc.SaveAs(tmCfg.GenesisFile())
	if err != nil {
		return "", err
	}

	pvStateFile := tmCfg.PrivValidatorStateFile()
	if err := tmos.EnsureDir(filepath.Dir(pvStateFile), 0o777); err != nil {
		return "", err
	}
	pvKeyFile := tmCfg.PrivValidatorKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(pvKeyFile), 0o777); err != nil {
		return "", err
	}
	filePV := privval.NewFilePV(val.ConsensusKey, pvKeyFile, pvStateFile)
	filePV.Save()

	nodeKeyFile := tmCfg.NodeKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(nodeKeyFile), 0o777); err != nil {
		return "", err
	}
	nodeKey := &p2p.NodeKey{
		PrivKey: val.NetworkKey,
	}
	if err := nodeKey.SaveAs(nodeKeyFile); err != nil {
		return "", err
	}

	return basePath, nil
}
