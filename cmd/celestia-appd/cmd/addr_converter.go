package cmd

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// AddrConversionCmd returns a command that converts between celestia1xxx and
// celestiavaloper1xxx addresses.
func AddrConversionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addr-conversion [celestia address]",
		Short: "Convert a celestia1xxx address to a validator operator address celestiavaloper1xxx",
		Long:  `Reads a celestia1xxx or celestiavaloper1xxx address and converts it to the other type.`,
		Example: "celestia-appd addr-conversion celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z\n" +
			"celestia-appd addr-conversion celestiavaloper1grvklux2yjsln7ztk6slv538396qatck9gj7vy\n",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			converted, err := Convert(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", converted)
			return nil
		},
	}
	return cmd
}

func Convert(original string) (string, error) {
	acc, err := sdk.AccAddressFromBech32(original)
	if err == nil {
		return sdk.ValAddress(acc.Bytes()).String(), nil
	}
	valAddr, err := sdk.ValAddressFromBech32(original)
	if err == nil {
		return sdk.AccAddress(valAddr.Bytes()).String(), nil
	}
	return "", fmt.Errorf("invalid address: %s", original)
}
