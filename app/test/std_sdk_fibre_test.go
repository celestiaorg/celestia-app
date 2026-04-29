//go:build fibre

package app_test

import (
	"slices"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/user"
	"github.com/celestiaorg/celestia-app/v9/test/util/blobfactory"
	valaddrtypes "github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func (s *StandardSDKIntegrationTestSuite) TestFibreProviderTxAndQuery() {
	t := s.T()
	require := s.Require()

	t.Run("set fibre provider host", func(t *testing.T) {
		valAccount := s.getValidatorAccount()
		msg := &valaddrtypes.MsgSetFibreProviderInfo{
			Signer: valAccount.String(),
			Host:   "www.provider.com:7980",
		}
		txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(s.getValidatorName()))
		require.NoError(err)
		res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, res.Code)
	})

	t.Run("query valaddr fibre provider info", func(t *testing.T) {
		valAccount := s.getValidatorAccount()
		testAddress := "provider.example.com:7980"
		msg := &valaddrtypes.MsgSetFibreProviderInfo{
			Signer: valAccount.String(),
			Host:   testAddress,
		}
		txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAccount(s.getValidatorName()))
		require.NoError(err)
		res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
		require.NoError(err)
		require.Equal(abci.CodeTypeOK, res.Code)

		queryClient := valaddrtypes.NewQueryClient(s.cctx.GRPCClient)
		allProvidersResp, err := queryClient.AllFibreProviders(s.cctx.GoContext(), &valaddrtypes.QueryAllFibreProvidersRequest{})
		require.NoError(err)
		require.NotNil(allProvidersResp)
		require.Equal(0, slices.IndexFunc(allProvidersResp.Providers, func(provider valaddrtypes.FibreProvider) bool {
			return provider.Info.Host == testAddress
		}))

		infoResp, err := queryClient.FibreProviderInfo(s.cctx.GoContext(), &valaddrtypes.QueryFibreProviderInfoRequest{
			ValidatorConsensusAddress: allProvidersResp.Providers[0].ValidatorConsensusAddress,
		})
		require.NoError(err)
		require.True(infoResp.Found)
		require.NotNil(infoResp.Info)
		assert.Equal(t, testAddress, infoResp.Info.Host)
	})
}
