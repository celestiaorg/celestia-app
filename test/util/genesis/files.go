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

// InitFiles initializes the files for a new Comet node with the provided
// genesis. It will use the validatorIndex to save the validator's consensus
// key.
func InitFiles(
	rootDir string,
	tmConfig *config.Config,
	genesis *Genesis,
	validatorIndex int,
) error {
	val, has := genesis.Validator(validatorIndex)
	if !has {
		return fmt.Errorf("validator %d not found", validatorIndex)
	}

	tmConfig.SetRoot(rootDir)

	// save the genesis file
	configPath := filepath.Join(rootDir, "config")
	err := os.MkdirAll(configPath, os.ModePerm)
	if err != nil {
		return err
	}
	genesisDoc, err := genesis.Export()
	if err != nil {
		return fmt.Errorf("exporting genesis: %w", err)
	}
	err = genesisDoc.SaveAs(tmConfig.GenesisFile())
	if err != nil {
		return err
	}

	pvStateFile := tmConfig.PrivValidatorStateFile()
	if err := tmos.EnsureDir(filepath.Dir(pvStateFile), 0o777); err != nil {
		return err
	}
	pvKeyFile := tmConfig.PrivValidatorKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(pvKeyFile), 0o777); err != nil {
		return err
	}
	filePV := privval.NewFilePV(val.ConsensusKey, pvKeyFile, pvStateFile)
	filePV.Save()

	nodeKeyFile := tmConfig.NodeKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(nodeKeyFile), 0o777); err != nil {
		return err
	}
	nodeKey := &p2p.NodeKey{
		PrivKey: val.NetworkKey,
	}
	if err := nodeKey.SaveAs(nodeKeyFile); err != nil {
		return err
	}

	return nil
}
