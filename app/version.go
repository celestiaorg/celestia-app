package app

import (
	"fmt"

	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/x/blob"
	"github.com/celestiaorg/celestia-app/x/blobstream"
	"github.com/celestiaorg/celestia-app/x/mint"
	"github.com/celestiaorg/celestia-app/x/upgrade"
	upgradetypes "github.com/celestiaorg/celestia-app/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	authzmodule "github.com/cosmos/cosmos-sdk/x/authz/module"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/capability"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	"github.com/cosmos/cosmos-sdk/x/distribution"
	"github.com/cosmos/cosmos-sdk/x/evidence"
	feegrantmodule "github.com/cosmos/cosmos-sdk/x/feegrant/module"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/gov"
	"github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/slashing"
	"github.com/cosmos/cosmos-sdk/x/staking"
	"github.com/cosmos/ibc-go/v6/modules/apps/transfer"
	ibc "github.com/cosmos/ibc-go/v6/modules/core"
)

var (
	// versions that the current state machine supports
	supportedVersions = []uint64{v1.Version, v2.Version}

	v1moduleVersionMap = make(module.VersionMap)
	v2moduleVersionMap = make(module.VersionMap)
)

const DefaultInitialVersion = v1.Version

// this is used as a compile time consistency check across different module
// based maps
func init() {
	v1moduleVersionMap = module.VersionMap{
		"bank":         bank.AppModule{}.ConsensusVersion(),
		"auth":         auth.AppModule{}.ConsensusVersion(),
		"authz":        authzmodule.AppModule{}.ConsensusVersion(),
		"staking":      staking.AppModule{}.ConsensusVersion(),
		"mint":         mint.AppModule{}.ConsensusVersion(),
		"distribution": distribution.AppModule{}.ConsensusVersion(),
		"slashing":     slashing.AppModule{}.ConsensusVersion(),
		"gov":          gov.AppModule{}.ConsensusVersion(),
		"params":       params.AppModule{}.ConsensusVersion(),
		"vesting":      vesting.AppModule{}.ConsensusVersion(),
		"feegrant":     feegrantmodule.AppModule{}.ConsensusVersion(),
		"evidence":     evidence.AppModule{}.ConsensusVersion(),
		"crisis":       crisis.AppModule{}.ConsensusVersion(),
		"genutil":      genutil.AppModule{}.ConsensusVersion(),
		"capability":   capability.AppModule{}.ConsensusVersion(),
		"blob":         blob.AppModule{}.ConsensusVersion(),
		"qgb":          blobstream.AppModule{}.ConsensusVersion(),
		"ibc":          ibc.AppModule{}.ConsensusVersion(),
		"transfer":     transfer.AppModule{}.ConsensusVersion(),
	}

	// v2 has all the same modules as v1 with the addition of an upgrade module
	v2moduleVersionMap = make(module.VersionMap)
	for k, v := range v1moduleVersionMap {
		v2moduleVersionMap[k] = v
	}
	v2moduleVersionMap[upgradetypes.ModuleName] = upgrade.AppModule{}.ConsensusVersion()

	for moduleName := range ModuleBasics {
		isSupported := false
		for _, v := range supportedVersions {
			versionMap := GetModuleVersion(v)
			if _, ok := versionMap[moduleName]; ok {
				isSupported = true
				break
			}
		}
		if !isSupported {
			panic(fmt.Sprintf("inconsistency: module %s not found in any version", moduleName))
		}
	}
}

func IsSupported(version uint64) bool {
	for _, v := range supportedVersions {
		if v == version {
			return true
		}
	}
	return false
}

func GetModuleVersion(appVersion uint64) module.VersionMap {
	switch appVersion {
	case v1.Version:
		return v1moduleVersionMap
	case v2.Version:
		return v2moduleVersionMap
	default:
		panic(fmt.Sprintf("unsupported app version %d", appVersion))
	}
}
