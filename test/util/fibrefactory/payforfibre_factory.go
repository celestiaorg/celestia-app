// Package fibrefactory builds signed MsgPayForFibre transactions carrying
// realistic multi-validator certificates. blobfactory.NewMsgPayForFibre covers
// the single-signature case; this package covers a real quorum.
package fibrefactory

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// EscrowAmount funds each generated signer's escrow account. A PayForFibre for
// a DefaultBlobSize blob costs far less, so no transaction runs out of escrow.
const EscrowAmount = 10_000_000

// Fixture is an app with a bonded validator set and funded escrow accounts,
// together with everything needed to sign PayForFibre certificates against it.
type Fixture struct {
	App *app.App
	// ValPrivs holds the validator consensus keys in the order the fibre
	// keeper reads the validator set. Certificates are positional over it.
	ValPrivs []ed25519.PrivKey
	// Signer holds every funded escrow account. It is one signer rather than
	// one per account because user.NewSigner lists the whole keyring on
	// construction, which makes a signer per account quadratic.
	Signer *user.Signer
	// Accounts names the funded escrow accounts.
	Accounts []string
	// BlockTime is the time the fixture committed its last block at, and the
	// time generated promises are created at.
	BlockTime time.Time
	// PromiseHeight is the height generated promises refer to.
	PromiseHeight int64

	// genesisState is kept so NewApp can rebuild an identical app.
	genesisState app.GenesisState
	// escrowAddresses are the accounts NewApp re-funds.
	escrowAddresses []sdk.AccAddress
	// valSet is kept so a rebuilt app produces identical block headers.
	valSet *tmtypes.ValidatorSet
}

// NewFixture returns an app with `validators` bonded validators of equal voting
// power and `accounts` funded escrow accounts.
func NewFixture(tb testing.TB, accounts, validators int) *Fixture {
	tb.Helper()

	names := testfactory.GenerateAccounts(accounts)
	testApp := testutil.NewTestApp()
	genesisState, valSet, valPrivs, kr := testutil.GenesisStateWithValidators(testApp, validators, names...)

	fixture := &Fixture{
		Accounts:        names,
		BlockTime:       testutil.GenesisTime.Add(time.Minute),
		genesisState:    genesisState,
		escrowAddresses: make([]sdk.AccAddress, accounts),
		valSet:          valSet,
	}
	for i, name := range names {
		fixture.escrowAddresses[i] = testfactory.GetAddress(kr, name)
	}

	fixture.App = fixture.initApp(tb, testApp)
	fixture.PromiseHeight = fixture.App.LastBlockHeight()
	fixture.ValPrivs = ValidatorKeysInSetOrder(tb, fixture.App, fixture.PromiseHeight, valPrivs)

	userAccounts := make([]*user.Account, accounts)
	for i, name := range names {
		acc := testutil.DirectQueryAccount(fixture.App, fixture.escrowAddresses[i])
		userAccounts[i] = user.NewAccount(name, acc.GetAccountNumber(), acc.GetSequence())
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, userAccounts...)
	require.NoError(tb, err)
	fixture.Signer = signer

	return fixture
}

// NewApp returns a fresh app at the same genesis, with the same escrow funding
// and the same promise height, so transactions already built from this fixture
// stay valid against it. FinalizeBlock writes into the root store even without
// Commit, so benchmarking either one needs a new app per measurement; this
// rebuilds only the app and leaves the certificates alone.
func (f *Fixture) NewApp(tb testing.TB) *app.App {
	tb.Helper()
	return f.initApp(tb, testutil.NewTestApp())
}

// initApp brings testApp up to the promise height and funds every escrow
// account.
func (f *Fixture) initApp(tb testing.TB, testApp *app.App) *app.App {
	tb.Helper()
	testApp = testutil.InitialiseTestAppWithGenesis(testApp, app.DefaultConsensusParams(), f.genesisState)

	// The genesis block, then one more so the promise height is committed and
	// its validator set is readable from HistoricalInfo.
	for _, blockTime := range []time.Time{testutil.GenesisTime, f.BlockTime} {
		_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:               blockTime,
			Height:             testApp.LastBlockHeight() + 1,
			Hash:               testApp.LastCommitID().Hash,
			NextValidatorsHash: f.valSet.Hash(),
		})
		require.NoError(tb, err)
		_, err = testApp.Commit()
		require.NoError(tb, err)
	}

	for _, addr := range f.escrowAddresses {
		SeedEscrow(tb, testApp, addr, EscrowAmount)
	}
	return testApp
}

// Quorum returns how many leading validator signatures a certificate needs.
func (f *Fixture) Quorum(tb testing.TB) int {
	return QuorumSize(tb, f.App, f.PromiseHeight)
}

// Txs returns one signed PayForFibre transaction per account, each carrying a
// certificate of `quorum` validator signatures and a distinct commitment.
func (f *Fixture) Txs(tb testing.TB, quorum int) [][]byte {
	tb.Helper()
	txs := make([][]byte, len(f.Accounts))
	for i, account := range f.Accounts {
		msg := NewMsgPayForFibreWithQuorum(tb, f.Signer, account, f.PromiseHeight,
			f.BlockTime, DistinctCommitment(i), f.ValPrivs, quorum)
		tx, _, err := f.Signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
		require.NoError(tb, err)
		txs[i] = tx
	}
	return txs
}

// NewMsgPayForFibreWithQuorum returns a MsgPayForFibre whose promise is signed
// by the signer account, and whose certificate carries the promise sign bytes
// signed by the first `quorum` keys of valPrivs. The signatures are positional
// over the validator set, so valPrivs must already be in set order.
func NewMsgPayForFibreWithQuorum(
	tb testing.TB,
	signer *user.Signer,
	account string,
	height int64,
	creationTime time.Time,
	commitment []byte,
	valPrivs []ed25519.PrivKey,
	quorum int,
) *fibretypes.MsgPayForFibre {
	tb.Helper()
	require.LessOrEqual(tb, quorum, len(valPrivs), "quorum exceeds the validator set")

	acc := signer.Account(account)
	pubKey, ok := acc.PubKey().(*secp256k1.PubKey)
	require.True(tb, ok, "payment promises are signed with secp256k1 keys")

	msg := blobfactory.NewMsgPayForFibre(tb, pubKey, testutil.ChainID)
	msg.PaymentPromise.Height = height
	msg.PaymentPromise.Commitment = commitment
	// Truncate to the second so the promise encodes identically whatever
	// monotonic clock reading it was built from.
	msg.PaymentPromise.CreationTimestamp = creationTime.Truncate(time.Second)

	// SignBytes covers every promise field but the signature, so both the
	// signer and the validators sign the same bytes.
	pp := fibre.PaymentPromise{}
	require.NoError(tb, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(tb, err)

	msg.PaymentPromise.Signature, _, err = signer.Keyring().Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(tb, err)

	signatures := make([][]byte, quorum)
	for i := range quorum {
		signatures[i], err = valPrivs[i].Sign(signBytes)
		require.NoError(tb, err)
	}
	msg.ValidatorSignatures = signatures
	return msg
}

// ValidatorKeysInSetOrder returns valPrivs reordered to match the validator set
// the fibre keeper reads at height. Staking re-sorts HistoricalInfo by voting
// power and then consensus address, and certificates are positional over that
// order, so genesis order is not the order that counts.
func ValidatorKeysInSetOrder(tb testing.TB, testApp *app.App, height int64, valPrivs []ed25519.PrivKey) []ed25519.PrivKey {
	tb.Helper()
	valset := historicalValset(tb, testApp, height)

	byPubKey := make(map[string]ed25519.PrivKey, len(valPrivs))
	for _, valPriv := range valPrivs {
		byPubKey[string(valPriv.PubKey().Bytes())] = valPriv
	}

	ordered := make([]ed25519.PrivKey, len(valset))
	for i, val := range valset {
		consPubKey, err := val.ConsPubKey()
		require.NoError(tb, err)
		valPriv, found := byPubKey[string(consPubKey.Bytes())]
		require.True(tb, found, "validator %d of the set at height %d has no key", i, height)
		ordered[i] = valPriv
	}
	return ordered
}

// QuorumSize returns how many leading validator signatures a certificate needs
// at height. It mirrors the keeper: voting power is staked tokens, and the
// threshold is two thirds of the total.
func QuorumSize(tb testing.TB, testApp *app.App, height int64) int {
	tb.Helper()
	valset := historicalValset(tb, testApp, height)

	var total int64
	for _, val := range valset {
		total += val.Tokens.Int64()
	}
	minRequired := total * 2 / 3

	var power int64
	for i, val := range valset {
		power += val.Tokens.Int64()
		if power >= minRequired {
			return i + 1
		}
	}
	return len(valset)
}

// SeedEscrow funds owner's escrow account so its PayForFibre transactions can
// settle.
func SeedEscrow(tb testing.TB, testApp *app.App, owner sdk.AccAddress, amount int64) {
	tb.Helper()
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
	})
	balance := sdk.NewInt64Coin(appconsts.BondDenom, amount)
	require.NoError(tb, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, owner, fibretypes.ModuleName, sdk.NewCoins(balance)))
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           owner.String(),
		Balance:          balance,
		AvailableBalance: balance,
	})
}

// DistinctCommitment returns the commitment for index i. Promises must hash
// differently or the double-spend check rejects every one after the first.
func DistinctCommitment(i int) []byte {
	commitment := make([]byte, share.FibreCommitmentSize)
	binary.BigEndian.PutUint64(commitment, uint64(i))
	return commitment
}

func historicalValset(tb testing.TB, testApp *app.App, height int64) []stakingtypes.Validator {
	tb.Helper()
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
	})
	info, err := testApp.StakingKeeper.GetHistoricalInfo(ctx, height)
	require.NoError(tb, err)
	return info.Valset
}
