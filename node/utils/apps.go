package utils

import (
	"os"

	appV2 "github.com/celestiaorg/celestia-app/v2/app"
	encodingV2 "github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	appV3 "github.com/celestiaorg/celestia-app/v3/app"
	encodingV3 "github.com/celestiaorg/celestia-app/v3/app/encoding"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/tendermint/tendermint/libs/log"
	tmdb "github.com/tendermint/tm-db"
)

const (
	// upgradeHeightV2 is the height at which the app should be upgraded to v2.
	upgradeHeightV2 = int64(5)
	// upgradeHeightV2 is the height at which the app should be upgraded to v3.
	upgradeHeightV3 = int64(10)
)

func GetApplications() map[uint64]AppWithMigrations {
	appV2 := NewAppV2()
	appV3 := NewAppV3()

	return map[uint64]AppWithMigrations{
		v1.Version: appV2,
		v2.Version: appV2,
		v3.Version: appV3,
	}
}

func NewAppV2() *appV2.App {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := encodingV2.MakeConfig(appV2.ModuleEncodingRegisters...)
	appOptions := NoopAppOptions{}
	return appV2.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeightV2, upgradeHeightV3, appOptions)
}

func NewAppV3() *appV3.App {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := encodingV3.MakeConfig(appV3.ModuleEncodingRegisters...)
	appOptions := NoopAppOptions{}
	return appV3.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeightV2, appOptions)
}
