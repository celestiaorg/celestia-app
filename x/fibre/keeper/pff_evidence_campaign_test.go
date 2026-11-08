//go:build pffcampaign

package keeper_test

import (
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func campaignEvidence(ctx sdk.Context) sdk.Context { return types.WithVerificationEvidence(ctx) }
