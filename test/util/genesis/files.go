package genesis

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cometbft/cometbft/config"
	cmtos "github.com/cometbft/cometbft/libs/os"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
)

// InitFiles initializes the files for a new Comet node with the provided
// genesis. It will use the validatorIndex to save the validator's consensus
// key.
func InitFiles(
	rootDir string,
	tmConfig *config.Config,
	appCfg *srvconfig.Config,
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

	genesisDocBz, err := genesis.ExportBytes()
	if err != nil {
		return fmt.Errorf("exporting genesis: %w", err)
	}

	err = cmtos.WriteFile(tmConfig.GenesisFile(), genesisDocBz, 0o644)
	if err != nil {
		return err
	}

	pvStateFile := tmConfig.PrivValidatorStateFile()
	if err := os.MkdirAll(filepath.Dir(pvStateFile), 0o777); err != nil {
		return fmt.Errorf("could not create directory %q: %w", filepath.Dir(pvStateFile), err)
	}

	pvKeyFile := tmConfig.PrivValidatorKeyFile()
	if err := os.MkdirAll(filepath.Dir(pvKeyFile), 0o777); err != nil {
		return fmt.Errorf("could not create directory %q: %w", filepath.Dir(pvKeyFile), err)
	}

	filePV := privval.NewFilePV(val.ConsensusKey, pvKeyFile, pvStateFile)
	filePV.Save()

	nodeKeyFile := tmConfig.NodeKeyFile()
	if err := os.MkdirAll(filepath.Dir(nodeKeyFile), 0o777); err != nil {
		return fmt.Errorf("could not create directory %q: %w", filepath.Dir(nodeKeyFile), err)
	}
	nodeKey := &p2p.NodeKey{
		PrivKey: val.NetworkKey,
	}
	if err := nodeKey.SaveAs(nodeKeyFile); err != nil {
		return err
	}

	appConfigFilePath := filepath.Join(rootDir, "config", "app.toml")
	srvconfig.WriteConfigFile(appConfigFilePath, appCfg)

	config.WriteConfigFile(filepath.Join(rootDir, "config", "config.toml"), tmConfig)

	return nil
}
