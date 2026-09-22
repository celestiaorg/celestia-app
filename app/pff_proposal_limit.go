package app

import (
	"fmt"
	"strconv"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
)

// FlagPFFProposalLimit limits locally proposed PFF messages. Zero uses the protocol limit.
const FlagPFFProposalLimit = "pff-proposal-limit"

func parsePFFProposalLimit(value interface{}) (int, error) {
	if value == nil {
		return 0, nil
	}
	limit, err := strconv.Atoi(fmt.Sprint(value))
	if err != nil || limit < 0 || limit > appconsts.GetMaxPayForFibreMessages(appconsts.Version) {
		return 0, fmt.Errorf("%s must be an integer between 0 and %d", FlagPFFProposalLimit, appconsts.GetMaxPayForFibreMessages(appconsts.Version))
	}
	return limit, nil
}
