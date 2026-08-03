package keeper_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkValidatePayForFibreSignatures(b *testing.B) {
	for _, validatorCount := range []int{1, 10, 50, 100} {
		b.Run(fmt.Sprintf("%d_validators", validatorCount), func(b *testing.B) {
			ctx, k, msg := setupBenchmarkPayForFibreValidation(b, validatorCount)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				require.NoError(b, k.ValidatePayForFibreSignatures(ctx, msg))
			}
		})
	}
}

func setupBenchmarkPayForFibreValidation(b *testing.B, validatorCount int) (sdk.Context, *keeper.Keeper, *types.MsgPayForFibre) {
	b.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(b, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, cmtproto.Header{
		ChainID: "test-chain",
		Height:  100,
		Time:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}, false, log.NewNopLogger())

	msg := newBenchmarkPayForFibreMsg(b, ctx)
	historicalInfo, valKeys := newBenchmarkHistoricalInfo(b, ctx, validatorCount)
	signBenchmarkPayForFibreValidators(b, msg, valKeys)

	stakingKeeper := &MockStakingKeeper{
		historicalInfo: map[int64]stakingtypes.HistoricalInfo{
			ctx.BlockHeight(): historicalInfo,
		},
	}
	k := keeper.NewKeeper(cdc, storeKey, &MockBankKeeper{}, stakingKeeper, authtypes.NewModuleAddress("gov").String())
	k.SetParams(ctx, types.DefaultParams())

	return ctx, k, msg
}

func newBenchmarkPayForFibreMsg(b *testing.B, ctx sdk.Context) *types.MsgPayForFibre {
	b.Helper()

	signerPrivKey := secp256k1.GenPrivKey()
	signerPubKey := signerPrivKey.PubKey().(*secp256k1.PubKey)
	promise := types.PaymentPromise{
		ChainId:           ctx.ChainID(),
		Height:            ctx.BlockHeight(),
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          1000,
		BlobVersion:       0,
		Commitment:        make([]byte, fibre.CommitmentSize),
		CreationTimestamp: ctx.BlockTime(),
		SignerPublicKey:   *signerPubKey,
	}

	pp := fibre.PaymentPromise{}
	require.NoError(b, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(b, err)
	promise.Signature, err = signerPrivKey.Sign(signBytes)
	require.NoError(b, err)

	return &types.MsgPayForFibre{
		Signer:         sdk.AccAddress(signerPubKey.Address()).String(),
		PaymentPromise: promise,
	}
}

func newBenchmarkHistoricalInfo(b *testing.B, ctx sdk.Context, validatorCount int) (stakingtypes.HistoricalInfo, []ed25519.PrivKey) {
	b.Helper()

	vals := make([]stakingtypes.Validator, validatorCount)
	valKeys := make([]ed25519.PrivKey, validatorCount)
	for i := range validatorCount {
		valPrivKey := ed25519.GenPrivKey()
		valKeys[i] = valPrivKey

		pk, err := cryptocodec.FromCmtPubKeyInterface(valPrivKey.PubKey())
		require.NoError(b, err)
		anyPubKey, err := codectypes.NewAnyWithValue(pk)
		require.NoError(b, err)

		vals[i] = stakingtypes.Validator{
			OperatorAddress: sdk.ValAddress(valPrivKey.PubKey().Address()).String(),
			ConsensusPubkey: anyPubKey,
			Tokens:          math.NewInt(3),
		}
	}

	return stakingtypes.HistoricalInfo{
		Header: cmtproto.Header{
			Height: ctx.BlockHeight(),
			Time:   ctx.BlockTime(),
		},
		Valset: vals,
	}, valKeys
}

func signBenchmarkPayForFibreValidators(b *testing.B, msg *types.MsgPayForFibre, valKeys []ed25519.PrivKey) {
	b.Helper()

	pp := fibre.PaymentPromise{}
	require.NoError(b, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(b, err)

	quorum := (len(valKeys)*2)/3 + 1
	msg.ValidatorSignatures = make([][]byte, quorum)
	for i := range quorum {
		msg.ValidatorSignatures[i], err = valKeys[i].Sign(signBytes)
		require.NoError(b, err)
	}
}
