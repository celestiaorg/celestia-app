package keeper_test

import (
	"encoding/hex"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/ethidentity/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/ethidentity/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestIndexPubKeyIndexesSameKeyIdentity(t *testing.T) {
	k, ctx := setupKeeper(t)
	pubKey := testSecpPubKey(t)

	ethAddr, err := keeper.EthereumAddressFromPubKey(pubKey)
	require.NoError(t, err)

	require.NoError(t, k.IndexPubKey(ctx, pubKey))

	celAddr, found := k.Resolve(ctx, ethAddr)
	require.True(t, found)
	require.Equal(t, sdk.AccAddress(pubKey.Address()), celAddr)
}

func TestIndexPubKeyIsIdempotent(t *testing.T) {
	k, ctx := setupKeeper(t)
	pubKey := testSecpPubKey(t)

	require.NoError(t, k.IndexPubKey(ctx, pubKey))
	require.NoError(t, k.IndexPubKey(ctx, pubKey))
}

func TestIndexPubKeyIgnoresNonSecp256k1Keys(t *testing.T) {
	k, ctx := setupKeeper(t)
	pubKey := ed25519.GenPrivKey().PubKey()

	require.NoError(t, k.IndexPubKey(ctx, pubKey))

	genesis := k.ExportGenesis(ctx)
	require.Empty(t, genesis.Mappings)
}

func TestGenesisRoundTrip(t *testing.T) {
	k, ctx := setupKeeper(t)
	pubKey := testSecpPubKey(t)

	ethAddr, err := keeper.EthereumAddressFromPubKey(pubKey)
	require.NoError(t, err)
	celAddr := sdk.AccAddress(pubKey.Address())

	genesis := types.GenesisState{Mappings: []types.Mapping{{
		EthereumAddress: common.BytesToAddress(ethAddr).Hex(),
		CelestiaAddress: celAddr.String(),
	}}}
	require.NoError(t, k.InitGenesis(ctx, genesis))
	exported := k.ExportGenesis(ctx)
	require.Equal(t, genesis, exported)
}

func setupKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := testutil.DefaultContextWithDB(t, storeKey, tStoreKey)
	return keeper.NewKeeper(storeKey), testCtx.Ctx
}

func testSecpPubKey(t *testing.T) *secp256k1.PubKey {
	t.Helper()
	privBytes, err := hex.DecodeString("4c0883a69102937d6231471b5dbb6204fe512961708279b727a63ca9b9a4b4f3")
	require.NoError(t, err)
	privKey, err := gethcrypto.ToECDSA(privBytes)
	require.NoError(t, err)
	return &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&privKey.PublicKey)}
}
