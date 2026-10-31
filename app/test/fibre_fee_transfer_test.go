package app_test

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"cosmossdk.io/store/rootmulti"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// feeTransferBlockTime is the time of the block that settles the payments.
var feeTransferBlockTime = testutil.GenesisTime.Add(time.Hour)

var pffGas = []user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}

// feeTransferGenesis is one genesis replayed at several app versions, so the
// resulting chains differ only in the version.
type feeTransferGenesis struct {
	genesis  app.GenesisState
	valsHash []byte
	kr       keyring.Keyring
	accounts []string
	owner    *secp256k1.PrivKey // escrow owner of the timed-out promises
}

func newFeeTransferGenesis(t *testing.T) *feeTransferGenesis {
	accounts := testfactory.GenerateAccounts(3)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(testutil.NewTestApp(), accounts...)
	return &feeTransferGenesis{genesis: genesis, valsHash: valSet.Hash(), kr: kr, accounts: accounts, owner: secp256k1.GenPrivKey()}
}

// start initialises a chain at appVersion, commits block 1 and seeds the
// escrows: moduleFunds moves into the fibre module, escrow records escrowAmount each.
func (g *feeTransferGenesis) start(t *testing.T, appVersion uint64, moduleFunds, escrowAmount int64) *app.App {
	t.Helper()
	cp := app.DefaultConsensusParams()
	cp.Version.App = appVersion
	testApp := testutil.InitialiseTestAppWithGenesis(testutil.NewTestApp(), cp, g.genesis)
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time: testutil.GenesisTime, Height: 1, Hash: testApp.LastCommitID().Hash, NextValidatorsHash: g.valsHash,
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)

	ctx := testApp.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: testApp.LastBlockHeight()})
	funder := testfactory.GetAddress(g.kr, g.accounts[2])
	require.NoError(t, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, funder, fibretypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, moduleFunds))))
	escrow := sdk.NewInt64Coin(appconsts.BondDenom, escrowAmount)
	for _, owner := range g.escrowOwners() {
		testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: owner.String(), Balance: escrow, AvailableBalance: escrow})
	}
	return testApp
}

func (g *feeTransferGenesis) escrowOwners() []sdk.AccAddress {
	return []sdk.AccAddress{
		testfactory.GetAddress(g.kr, g.accounts[0]),
		testfactory.GetAddress(g.kr, g.accounts[1]),
		sdk.AccAddress(g.owner.PubKey().Address()),
	}
}

func (g *feeTransferGenesis) signer(t *testing.T, testApp *app.App, index int) *user.Signer {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	return newSignerFactory(t, g.kr, enc.TxConfig, g.accounts, queryAccountInfo(testApp, g.accounts, g.kr))(index)
}

// timeoutMsg returns a MsgPaymentPromiseTimeout, signed by account 2, for an
// expired promise of the owner escrow created at creation.
func (g *feeTransferGenesis) timeoutMsg(t *testing.T, testApp *app.App, creation time.Time) *fibretypes.MsgPaymentPromiseTimeout {
	t.Helper()
	promise := blobfactory.NewMsgPayForFibre(t, g.owner.PubKey().(*secp256k1.PubKey), testutil.ChainID).PaymentPromise
	promise.CreationTimestamp = creation
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = g.owner.Sign(signBytes)
	require.NoError(t, err)
	return &fibretypes.MsgPaymentPromiseTimeout{Signer: testfactory.GetAddress(g.kr, g.accounts[2]).String(), PaymentPromise: promise}
}

// settle finalizes and commits one block of txs at feeTransferBlockTime.
func settle(t *testing.T, testApp *app.App, txs [][]byte) *abci.ResponseFinalizeBlock {
	t.Helper()
	return commitBlockAt(t, testApp, feeTransferBlockTime, txs...)
}

// fibreTransfers returns the amounts of the bank transfers from the fibre module to the fee collector.
func fibreTransfers(events []abci.Event) []string {
	from := authtypes.NewModuleAddress(fibretypes.ModuleName).String()
	to := authtypes.NewModuleAddress(authtypes.FeeCollectorName).String()
	var amounts []string
	for _, event := range events {
		if event.Type != banktypes.EventTypeTransfer {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range event.Attributes {
			attrs[attr.Key] = attr.Value
		}
		if attrs[banktypes.AttributeKeySender] == from && attrs[banktypes.AttributeKeyRecipient] == to {
			amounts = append(amounts, attrs[sdk.AttributeKeyAmount])
		}
	}
	return amounts
}

// storeHashes returns the committed hash of every store at the last height.
func storeHashes(t *testing.T, testApp *app.App) map[string][]byte {
	t.Helper()
	info, err := testApp.CommitMultiStore().(*rootmulti.Store).GetCommitInfo(testApp.LastBlockHeight())
	require.NoError(t, err)
	hashes := map[string][]byte{}
	for _, store := range info.StoreInfos {
		hashes[store.Name] = store.CommitId.Hash
	}
	return hashes
}

// versionStores are the stores that differ between two chains run at different
// app versions: consensus holds the version, staking's historical info the app hash.
var versionStores = []string{consensustypes.StoreKey, stakingtypes.StoreKey}

// differingStores lists the stores whose committed hash differs between a and b.
func differingStores(t *testing.T, a, b *app.App) []string {
	t.Helper()
	hashesA, hashesB := storeHashes(t, a), storeHashes(t, b)
	require.Len(t, hashesB, len(hashesA))
	var names []string
	for name, hash := range hashesA {
		if string(hashesB[name]) != string(hash) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// TestFibreFeeTransferPerBlock settles one block at v10 and at v11 from the
// same genesis: v10 sends each payment to the fee collector inside its tx, v11
// sends their sum once in EndBlock, and every store but versionStores commits
// the same hash.
func TestFibreFeeTransferPerBlock(t *testing.T) {
	g := newFeeTransferGenesis(t)
	payment := fibretypes.PaymentAmount(100)
	chains := map[uint64]*app.App{}
	results := map[uint64]*abci.ResponseFinalizeBlock{}
	var txs [][]byte
	for _, appVersion := range []uint64{10, fibrekeeper.BatchedFeeTransferVersion} {
		testApp := g.start(t, appVersion, 3*pffTestEscrowAmount, pffTestEscrowAmount)
		if txs == nil {
			creation := feeTransferBlockTime.Add(-time.Second)
			timeout := g.timeoutMsg(t, testApp, feeTransferBlockTime.Add(-2*time.Hour))
			// Settles a timeout, then fails: the deferred payment must roll back with the tx.
			reverted := g.timeoutMsg(t, testApp, feeTransferBlockTime.Add(-3*time.Hour))
			tooMuch := banktypes.NewMsgSend(testfactory.GetAddress(g.kr, g.accounts[2]), testfactory.GetAddress(g.kr, g.accounts[0]),
				sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1<<62)))
			signer2 := g.signer(t, testApp, 2)
			timeoutTx, _, err := signer2.CreateTx([]sdk.Msg{timeout}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
			require.NoError(t, err)
			require.NoError(t, signer2.IncrementSequence(g.accounts[2]))
			revertedTx, _, err := signer2.CreateTx([]sdk.Msg{reverted, tooMuch}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
			require.NoError(t, err)
			txs = [][]byte{
				newSignedPayForFibreTxAt(t, g.signer(t, testApp, 0), g.accounts[0], true, creation, pffGas...),
				newSignedPayForFibreTxAt(t, g.signer(t, testApp, 1), g.accounts[1], true, creation, pffGas...),
				timeoutTx,
				revertedTx,
			}
		}
		results[appVersion] = settle(t, testApp, txs)
		chains[appVersion] = testApp
	}

	v10, v11 := results[10], results[fibrekeeper.BatchedFeeTransferVersion]
	for i := range txs {
		require.Equal(t, v10.TxResults[i].Code, v11.TxResults[i].Code, "tx %d", i)
		require.Equal(t, v10.TxResults[i].Log, v11.TxResults[i].Log, "tx %d", i)
		if i < 3 {
			require.Equal(t, abci.CodeTypeOK, v11.TxResults[i].Code, "tx %d: %s", i, v11.TxResults[i].Log)
			require.Equal(t, []string{payment.String()}, fibreTransfers(v10.TxResults[i].Events), "v10 tx %d", i)
			require.Less(t, v11.TxResults[i].GasUsed, v10.TxResults[i].GasUsed, "tx %d", i)
		} else {
			require.NotEqual(t, abci.CodeTypeOK, v11.TxResults[i].Code)
		}
		require.Empty(t, fibreTransfers(v11.TxResults[i].Events), "v11 tx %d", i)
	}
	require.Empty(t, fibreTransfers(v10.Events))
	total := sdk.NewCoin(payment.Denom, payment.Amount.MulRaw(3))
	require.Equal(t, []string{total.String()}, fibreTransfers(v11.Events))

	require.Equal(t, versionStores, differingStores(t, chains[10], chains[fibrekeeper.BatchedFeeTransferVersion]))
	for _, testApp := range chains {
		ctx := testApp.NewContext(true)
		var escrowed int64
		for _, owner := range g.escrowOwners() {
			escrow, found := testApp.FibreKeeper.GetEscrowAccount(ctx, owner.String())
			require.True(t, found)
			escrowed += escrow.Balance.Amount.Int64()
		}
		module := testApp.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(fibretypes.ModuleName), appconsts.BondDenom)
		require.Equal(t, escrowed, module.Amount.Int64(), "module balance must equal the escrow balances")
		require.Equal(t, 3*pffTestEscrowAmount-total.Amount.Int64(), escrowed)
	}
}

// TestFibreFeeTransferInsufficientModuleBalance underfunds the fibre module so
// the second payment of a block cannot be paid: v11 must reject it exactly as
// the v10 per-payment send does, and accept it when the module holds exactly enough.
func TestFibreFeeTransferInsufficientModuleBalance(t *testing.T) {
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	for _, tc := range []struct {
		moduleFunds int64
		secondOK    bool
	}{
		{moduleFunds: 2*payment - 1, secondOK: false},
		{moduleFunds: 2 * payment, secondOK: true},
	} {
		t.Run(fmt.Sprintf("module=%d", tc.moduleFunds), func(t *testing.T) {
			g := newFeeTransferGenesis(t)
			chains := map[uint64]*app.App{}
			results := map[uint64]*abci.ResponseFinalizeBlock{}
			var txs [][]byte
			for _, appVersion := range []uint64{10, fibrekeeper.BatchedFeeTransferVersion} {
				testApp := g.start(t, appVersion, tc.moduleFunds, pffTestEscrowAmount)
				if txs == nil {
					creation := feeTransferBlockTime.Add(-time.Second)
					txs = [][]byte{
						newSignedPayForFibreTxAt(t, g.signer(t, testApp, 0), g.accounts[0], true, creation, pffGas...),
						newSignedPayForFibreTxAt(t, g.signer(t, testApp, 1), g.accounts[1], true, creation, pffGas...),
					}
				}
				results[appVersion] = settle(t, testApp, txs)
				chains[appVersion] = testApp
			}
			v10, v11 := results[10], results[fibrekeeper.BatchedFeeTransferVersion]
			for i := range txs {
				require.Equal(t, v10.TxResults[i].Code, v11.TxResults[i].Code, "tx %d", i)
				require.Equal(t, v10.TxResults[i].Log, v11.TxResults[i].Log, "tx %d", i)
			}
			require.Equal(t, abci.CodeTypeOK, v11.TxResults[0].Code, v11.TxResults[0].Log)
			if tc.secondOK {
				require.Equal(t, abci.CodeTypeOK, v11.TxResults[1].Code, v11.TxResults[1].Log)
			} else {
				require.Equal(t, sdkerrors.ErrInsufficientFunds.ABCICode(), v11.TxResults[1].Code)
				require.Contains(t, v11.TxResults[1].Log, "failed to send fibre payment to fee collector")
			}
			require.Equal(t, versionStores, differingStores(t, chains[10], chains[fibrekeeper.BatchedFeeTransferVersion]))
		})
	}
}
