package utils

import (
	"os"

	appV1 "github.com/celestiaorg/celestia-app/app"
	encodingV1 "github.com/celestiaorg/celestia-app/app/encoding"
	appV2 "github.com/celestiaorg/celestia-app/v2/app"
	encodingV2 "github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/tendermint/tendermint/libs/log"
	tmdb "github.com/tendermint/tm-db"
)

const (
	upgradeHeightV1ToV2 = int64(3)
)

func GetApplications() map[uint64]AppWithMigrations {
	return map[uint64]AppWithMigrations{
		v1.Version: NewAppV1(),
		v2.Version: NewAppV2(),
	}
}

func NewAppV1() *appV1.App {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	loadLatest := true
	skipUpgradeHeights := make(map[int64]bool)
	homePath := ""
	invCheckPeriod := uint(1)
	encodingConfig := encodingV1.MakeConfig(appV1.ModuleEncodingRegisters...)
	appOptions := NoopAppOptions{}
	return appV1.New(logger, db, traceStore, loadLatest, skipUpgradeHeights, homePath, invCheckPeriod, encodingConfig, appOptions)
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
