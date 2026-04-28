//go:build valaddr_wiring

package keeper_test

import (
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgSetFibreProviderInfo(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)

	validators, err := testApp.StakingKeeper.GetBondedValidatorsByPower(ctx)
	require.NoError(t, err)
	require.Greater(t, len(validators), 0)

	valAddrStr := validators[0].GetOperator()
	consPubKey, err := validators[0].ConsPubKey()
	require.NoError(t, err)
	consAddr := sdk.ConsAddress(consPubKey.Address())

	t.Run("valid DNS hostname with port", func(t *testing.T) {
		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddrStr,
			Host:   "validator1.fibre.example.com:7980",
		}

		err = msg.ValidateBasic()
		require.NoError(t, err)

		msgServer := keeper.NewMsgServerImpl(testApp.ValAddrKeeper)
		_, err = msgServer.SetFibreProviderInfo(ctx, msg)
		require.NoError(t, err)

		retrievedInfo, found := testApp.ValAddrKeeper.GetFibreProviderInfo(ctx, consAddr)
		require.True(t, found)
		require.Equal(t, msg.Host, retrievedInfo.Host)
	})

	t.Run("valid IP with port", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "10.0.0.1:8080",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("valid IPv6 with port", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		// IPv6 literals must be square-bracketed in host:port form, per
		// RFC 3986 / net.SplitHostPort.
		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "[2001:db8::1]:8080",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("rejects bare hostname without port", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "validator.example.com",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("rejects scheme prefix", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		// `http://host:port` is a real-world operator mistake. gRPC
		// can't dial it, so the chain should refuse to record it.
		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "http://10.0.0.1:7980",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("rejects dns:/// prefix", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		// Historically registered form. gRPC happens to accept it as a
		// resolver URI, but allowing it lets operators register other
		// schemes that gRPC won't dial. Keep the chain to one form.
		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "dns:///10.0.0.1:7980",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("rejects port zero", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "10.0.0.1:0",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("rejects non-numeric port", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "10.0.0.1:abc",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("empty host", func(t *testing.T) {
		valAddr := sdk.ValAddress("validator1")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   "",
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})

	t.Run("non-existent validator", func(t *testing.T) {
		// Create an arbitrary validator address (not a real validator)
		arbitraryValAddr := sdk.ValAddress("arbitrary_val_addr")

		msg := &types.MsgSetFibreProviderInfo{
			Signer: arbitraryValAddr.String(),
			Host:   "nonexistent.validator.com:7980",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)

		// Call the message server handler - should fail (validator not found)
		msgServer := keeper.NewMsgServerImpl(testApp.ValAddrKeeper)
		_, err = msgServer.SetFibreProviderInfo(ctx, msg)
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidValidator))
	})

	t.Run("host too long", func(t *testing.T) {
		valAddr := sdk.ValAddress([]byte("validator1"))

		// Create a host longer than 100 characters
		longHost := "2001:0db8:852001:0db8:85a3a3:0000:0000:8a2e:0370:7334:2001:0db8:85a3:0000:0000:8a2e:0370:7334:2001:0db8:85a3:extra:data:here"

		msg := &types.MsgSetFibreProviderInfo{
			Signer: valAddr.String(),
			Host:   longHost,
		}

		err := msg.ValidateBasic()
		require.Error(t, err)
		require.True(t, errors.Is(err, types.ErrInvalidHostAddress))
	})
}
