package main

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	coretypes "github.com/tendermint/tendermint/types"
)

func NewGenTestCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "gentest [genesis-file] [gentxs-dir]",
		Short: "Test genesis file and gentxs in CI by starting a single validator network and creating a delegate transaction to all validators.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			// load the genesis file
			genPath := args[0]
			doc, err := coretypes.GenesisDocFromFile(genPath)
			if err != nil {
				return err
			}

			gentxs, err := readGentxs(ecfg, args[1])
			if err != nil {
				return err
			}

			return GenTxTest(".", doc, gentxs)
		},
	}
	return command
}

func readGentxs(ecfg encoding.Config, dir string) ([]sdk.Tx, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var allFileContents [][]byte

	for _, file := range files {
		if !file.IsDir() {
			content, err := os.ReadFile(fmt.Sprintf("%s/%s", dir, file.Name()))
			if err != nil {
				return nil, err
			}
			allFileContents = append(allFileContents, content)
		}
	}

	gentxs := make([]sdk.Tx, len(allFileContents))

	for i, rawGenTx := range allFileContents {
		sdkTx, err := ecfg.TxConfig.TxJSONDecoder()(rawGenTx)
		if err != nil {
			return nil, err
		}
		gentxs[i] = sdkTx
	}

	return gentxs, nil
}
