package cmd

import (
	"fmt"

	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/appd"
)

func Versions() abci.Versions {
	v3, err := appd.New("v3", nil /* uses default celestia */)
	if err != nil {
		panic(fmt.Errorf("failed to create celestia-appd v3: %w", err))
	}

	return abci.Versions{
		"v3": {
			Appd:        v3,
			UntilHeight: -1, // disable nova for now.
		},
	}
}
