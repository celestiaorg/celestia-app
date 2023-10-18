package app

import (
	"fmt"

	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/x/blob"
	"github.com/celestiaorg/celestia-app/x/blobstream"
	"github.com/celestiaorg/celestia-app/x/mint"
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

	// There is currently complete parity between v1 and v2 modules, but this
	// will likely change
	v2moduleVersionMap = v1moduleVersionMap
)

const DefaultInitialVersion = v1.Version

// this is used as a compile time consistency check across different module
// based maps
func init() {
	for moduleName := range ModuleBasics {
		for _, v := range supportedVersions {
			versionMap := GetModuleVersion(v)
			if _, ok := versionMap[moduleName]; !ok {
				panic(fmt.Sprintf("inconsistency: module %s not found in module version map for version %d", moduleName, v))
			}
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
