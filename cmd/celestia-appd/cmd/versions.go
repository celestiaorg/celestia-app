package cmd

import (
	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/appd"
)

func Versions() abci.Versions {
	v3, err := appd.New("v3", nil /* uses default celestia */)
	_ = err // TODO: handle this error, explicitly ignoring this for now as ledger tests fail due to not having the binary
	return abci.Versions{
		{
			Name:        "v3",
			Appd:        v3,
			UntilHeight: -1, // disable nova for now.
		},
	}
}
