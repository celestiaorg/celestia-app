package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math/unsafe"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	cometconfig "github.com/cometbft/cometbft/config"
	comettypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/cosmos/go-bip39"
	"github.com/spf13/cobra"
)

const (
	// FlagOverwrite defines a flag to overwrite an existing genesis JSON file.
	FlagOverwrite = "overwrite"
	// FlagRecover defines a flag to initialize the private validator key from a specific seed.
	FlagRecover = "recover"
)

// InitCmd returns a command that creates the config files and genesis.json for
// a chain.
func InitCmd(capp *app.App) *cobra.Command {
	return initCmd(capp.BasicManager, app.NodeHome)
}

// initCmd returns a command that initializes all files needed for Tendermint
// and the respective application.
//
// This command was heavily inspired by the Cosmos SDK's init command.
func initCmd(basicManager module.BasicManager, defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init [moniker]",
		Short:   "Initialize configuration files for a Celestia consensus node",
		Long:    "This command creates a genesis file and the default configuration files for a consensus node.",
		Example: "celestia-appd init node-name --chain-id celestia",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			codec := clientCtx.Codec

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			config.SetRoot(clientCtx.HomeDir)

			chainID, _ := cmd.Flags().GetString(flags.FlagChainID)
			switch {
			case chainID != "":
			case clientCtx.ChainID != "":
				chainID = clientCtx.ChainID
			default:
				chainID = fmt.Sprintf("test-chain-%v", unsafe.Str(6))
			}

			// Get bip39 mnemonic
			var mnemonic string
			recover, _ := cmd.Flags().GetBool(FlagRecover)
			if recover {
				inBuf := bufio.NewReader(cmd.InOrStdin())
				value, err := input.GetString("Enter your bip39 mnemonic", inBuf)
				if err != nil {
					return err
				}

				mnemonic = value
				if !bip39.IsMnemonicValid(mnemonic) {
					return errors.New("invalid mnemonic")
				}
			}

			initialHeight, _ := cmd.Flags().GetInt64(flags.FlagInitHeight)
			if initialHeight < 1 {
				initialHeight = 1
			}

			nodeID, _, err := genutil.InitializeNodeValidatorFilesFromMnemonic(config, mnemonic)
			if err != nil {
				return err
			}

			config.Moniker = args[0]

			genesisFile := config.GenesisFile()
			overwrite, _ := cmd.Flags().GetBool(FlagOverwrite)

			// use os.Stat to check if the file exists
			_, err = os.Stat(genesisFile)
			if !overwrite && !os.IsNotExist(err) {
				return fmt.Errorf("genesis.json file already exists: %v", genesisFile)
			}

			appGenesisState := basicManager.DefaultGenesis(codec)
			appState, err := json.MarshalIndent(appGenesisState, "", " ")
			if err != nil {
				return errorsmod.Wrap(err, "Failed to marshal default genesis state")
			}

			appGenesis := &types.AppGenesis{}
			if _, err := os.Stat(genesisFile); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
			} else {
				appGenesis, err = types.AppGenesisFromFile(genesisFile)
				if err != nil {
					return errorsmod.Wrap(err, "Failed to read genesis doc from file")
				}
			}

			appGenesis.AppName = version.AppName
			appGenesis.AppVersion = version.Version
			appGenesis.ChainID = chainID
			appGenesis.AppState = appState
			appGenesis.InitialHeight = initialHeight
			appGenesis.Consensus = &types.ConsensusGenesis{
				Validators: nil,
				Params:     getConsensusParams(),
			}

			if err = genutil.ExportGenesisFile(appGenesis, genesisFile); err != nil {
				return errorsmod.Wrap(err, "Failed to export genesis file")
			}

			if isKnownChainID(chainID) {
				fmt.Printf("The chain ID %s is a public network.\nPlease download the genesis file via:\n\tcelestia-appd download-genesis %s\n", chainID, chainID)
			}

			cometconfig.WriteConfigFile(filepath.Join(config.RootDir, "config", "config.toml"), config)

			toPrint := newPrintInfo(config.Moniker, chainID, nodeID, "", appState)
			return displayInfo(toPrint)
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "node's home directory")
	cmd.Flags().BoolP(FlagOverwrite, "o", false, "overwrite the genesis.json file")
	cmd.Flags().Bool(FlagRecover, false, "provide seed phrase to recover existing key instead of creating")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created")
	cmd.Flags().Int64(flags.FlagInitHeight, 1, "specify the initial block height at genesis")

	return cmd
}

type printInfo struct {
	Moniker    string          `json:"moniker" yaml:"moniker"`
	ChainID    string          `json:"chain_id" yaml:"chain_id"`
	NodeID     string          `json:"node_id" yaml:"node_id"`
	GenTxsDir  string          `json:"gentxs_dir" yaml:"gentxs_dir"`
	AppMessage json.RawMessage `json:"app_message" yaml:"app_message"`
}

func newPrintInfo(moniker, chainID, nodeID, genTxsDir string, appMessage json.RawMessage) printInfo {
	return printInfo{
		Moniker:    moniker,
		ChainID:    chainID,
		NodeID:     nodeID,
		GenTxsDir:  genTxsDir,
		AppMessage: appMessage,
	}
}

func displayInfo(info printInfo) error {
	out, err := json.MarshalIndent(info, "", " ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(os.Stderr, "%s\n", out)
	return err
}

func getConsensusParams() *comettypes.ConsensusParams {
	params := comettypes.DefaultConsensusParams()
	params.Version.App = appconsts.Version
	return params
}
