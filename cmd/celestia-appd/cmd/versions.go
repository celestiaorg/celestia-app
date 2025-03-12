package cmd

import (
	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/appd"

	embedding "github.com/celestiaorg/celestia-app/v4/internal/embedding"
)

func Versions() abci.Versions {
	v3AppBinary, err := embedding.CelestiaAppV3()
	_ = err // TODO: handle this error, explicitly ignoring this for now as ledger tests fail due to not having the binary

	v3, err := appd.New("v3", v3AppBinary)
	_ = err // TODO: handle this error, explicitly ignoring this for now as ledger tests fail due to not having the binary
	return abci.Versions{
		{
			Name:        "v3",
			Appd:        v3,
			UntilHeight: -1, // disable nova for now.
		},
	}
}
