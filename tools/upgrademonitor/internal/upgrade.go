package internal

import upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"

func IsUpgradeable(response *upgradetypes.QueryVersionTallyResponse) bool {
	if response == nil {
		return false
	}
	return response.GetVotingPower() > response.ThresholdPower
}
