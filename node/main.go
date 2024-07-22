package main

import (
	"fmt"

	v1 "github.com/celestiaorg/celestia-app/app"
	v1encoding "github.com/celestiaorg/celestia-app/app/encoding"
	v2 "github.com/celestiaorg/celestia-app/v2/app"
	v2encoding "github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmdb "github.com/tendermint/tm-db"
)

func main() {
	currentAppVersion := uint64(1)
	apps := getApps()
	multiplexer := NewMultiplexer(currentAppVersion, apps)

	fmt.Printf("%v\n", multiplexer)
}

func getApps() []types.Application {
	v1 := NewAppV1()
	v2 := NewAppV2()
	return []types.Application{v1, v2}
}

func NewAppV1() *v1.App {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	loadLatest := true
	skipUpgradeHeights := make(map[int64]bool)
	homePath := ""
	invCheckPeriod := uint(1)
	encodingConfig := v1encoding.MakeConfig(v1.ModuleEncodingRegisters...)
	appOptions := NoopAppOptions{}

	return v1.New(logger, db, traceStore, loadLatest, skipUpgradeHeights, homePath, invCheckPeriod, encodingConfig, appOptions)
}

func NewAppV2() *v2.App {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	invCheckPeriod := uint(1)
	encodingConfig := v2encoding.MakeConfig(v2.ModuleEncodingRegisters...)
	upgradeHeight := int64(0)
	appOptions := NoopAppOptions{}

	return v2.New(logger, db, traceStore, invCheckPeriod, encodingConfig, upgradeHeight, appOptions)
}
