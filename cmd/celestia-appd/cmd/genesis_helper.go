package cmd

import (
	"github.com/cosmos/cosmos-sdk/server"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/spf13/cobra"
)

// AppGenesisToCometGenesisConverterCmd returns a command that converts the app genesis to comet genesis.
func AppGenesisToCometGenesisConverterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "convert-genesis",
		Short: "Convert app genesis to comet genesis",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			appGenesis, err := genutiltypes.AppGenesisFromFile(serverCtx.Config.GenesisFile())
			if err != nil {
				return err
			}

			genDoc, err := appGenesis.ToGenesisDoc()
			if err != nil {
				return err
			}

			if err := genDoc.ValidateAndComplete(); err != nil {
				return err
			}

			if err := genDoc.SaveAs(serverCtx.Config.GenesisFile()); err != nil {
				return err
			}

			cmd.Println("successfully converted app genesis to comet genesis")
			return nil
		},
	}
}
