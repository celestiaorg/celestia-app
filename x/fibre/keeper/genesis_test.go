package keeper_test

import (
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (suite *KeeperTestSuite) TestInitGenesis() {
	// Create genesis state with multiple providers
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	
	genState := types.GenesisState{
		Providers: []types.GenesisProvider{
			{
				ValidatorAddress: val1Addr.String(),
				Info: types.FibreProviderInfo{
					IpAddress: "192.168.1.1",
				},
			},
			{
				ValidatorAddress: val2Addr.String(),
				Info: types.FibreProviderInfo{
					IpAddress: "192.168.1.2",
				},
			},
		},
	}
	
	// Initialize genesis
	suite.keeper.InitGenesis(suite.ctx, genState)
	
	// Verify providers were set
	info1, found1 := suite.keeper.GetFibreProviderInfo(suite.ctx, val1Addr)
	suite.True(found1)
	suite.Equal("192.168.1.1", info1.IpAddress)
	
	info2, found2 := suite.keeper.GetFibreProviderInfo(suite.ctx, val2Addr)
	suite.True(found2)
	suite.Equal("192.168.1.2", info2.IpAddress)
}

func (suite *KeeperTestSuite) TestInitGenesis_EmptyState() {
	// Initialize with empty genesis state
	genState := types.GenesisState{
		Providers: []types.GenesisProvider{},
	}
	
	suite.keeper.InitGenesis(suite.ctx, genState)
	
	// Verify no providers exist
	providers, err := suite.keeper.GetAllActiveFibreProviders(suite.ctx)
	suite.NoError(err)
	suite.Empty(providers)
}

func (suite *KeeperTestSuite) TestExportGenesis() {
	// Set up multiple providers
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	
	info1 := types.FibreProviderInfo{IpAddress: "192.168.1.1"}
	info2 := types.FibreProviderInfo{IpAddress: "192.168.1.2"}
	
	suite.keeper.SetFibreProviderInfo(suite.ctx, val1Addr, info1)
	suite.keeper.SetFibreProviderInfo(suite.ctx, val2Addr, info2)
	
	// Export genesis
	exported := suite.keeper.ExportGenesis(suite.ctx)
	
	// Verify exported state
	suite.Len(exported.Providers, 2)
	
	// Create a map for easier verification
	providerMap := make(map[string]types.FibreProviderInfo)
	for _, provider := range exported.Providers {
		providerMap[provider.ValidatorAddress] = provider.Info
	}
	
	// Verify both providers are in the export
	suite.Contains(providerMap, val1Addr.String())
	suite.Contains(providerMap, val2Addr.String())
	suite.Equal("192.168.1.1", providerMap[val1Addr.String()].IpAddress)
	suite.Equal("192.168.1.2", providerMap[val2Addr.String()].IpAddress)
}

func (suite *KeeperTestSuite) TestExportGenesis_EmptyState() {
	// Export genesis with no providers
	exported := suite.keeper.ExportGenesis(suite.ctx)
	
	// Verify empty state
	suite.Empty(exported.Providers)
}

func (suite *KeeperTestSuite) TestInitAndExportGenesis_RoundTrip() {
	// Create initial genesis state
	val1Addr := sdk.ValAddress("validator1")
	val2Addr := sdk.ValAddress("validator2")
	
	originalGenState := types.GenesisState{
		Providers: []types.GenesisProvider{
			{
				ValidatorAddress: val1Addr.String(),
				Info: types.FibreProviderInfo{
					IpAddress: "192.168.1.1",
				},
			},
			{
				ValidatorAddress: val2Addr.String(),
				Info: types.FibreProviderInfo{
					IpAddress: "192.168.1.2",
				},
			},
		},
	}
	
	// Initialize genesis
	suite.keeper.InitGenesis(suite.ctx, originalGenState)
	
	// Export genesis
	exportedGenState := suite.keeper.ExportGenesis(suite.ctx)
	
	// Verify the exported state matches the original
	suite.Len(exportedGenState.Providers, 2)
	
	// Create maps for comparison
	originalMap := make(map[string]types.FibreProviderInfo)
	for _, provider := range originalGenState.Providers {
		originalMap[provider.ValidatorAddress] = provider.Info
	}
	
	exportedMap := make(map[string]types.FibreProviderInfo)
	for _, provider := range exportedGenState.Providers {
		exportedMap[provider.ValidatorAddress] = provider.Info
	}
	
	// Compare the maps
	suite.Equal(len(originalMap), len(exportedMap))
	for addr, originalInfo := range originalMap {
		exportedInfo, exists := exportedMap[addr]
		suite.True(exists, "Address %s should exist in exported state", addr)
		suite.Equal(originalInfo.IpAddress, exportedInfo.IpAddress)
	}
}