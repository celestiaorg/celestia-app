package migration

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func DisableBlobstream(ctx sdk.Context) error {
	fmt.Printf("disabling blobstream module\n")
	fmt.Printf("consensus params version %v\n", ctx.ConsensusParams().Version)
	return nil
}
