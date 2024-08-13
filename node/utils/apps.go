package utils

import (
	"os"

	appV2 "github.com/celestiaorg/celestia-app/v2/app"
	encodingV2 "github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/tendermint/tendermint/libs/log"
	tmdb "github.com/tendermint/tm-db"
)

const (
	upgradeHeightV1ToV2 = int64(5)
	upgradeHeightV2ToV3 = int64(10)
)

func GetApplications() map[uint64]AppWithMigrations {
	appV2 := NewAppV2()

	return map[uint64]AppWithMigrations{
		v1.Version: appV2,
		v2.Version: appV2,
		// TODO: add a v3 app here
	}
}

func NewAppV2() *appV2.App {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := encodingV2.MakeConfig(appV2.ModuleEncodingRegisters...)
	appOptions := NoopAppOptions{}
	return appV2.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeightV1ToV2, appOptions)
}
