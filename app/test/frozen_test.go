package app_test

import (
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/stretchr/testify/require"
)

var frozenCodespace = "frozen"

// frozenChain drives a test app through FinalizeBlock with signed txs.
type frozenChain struct {
	app *app.App
	kr  keyring.Keyring
	enc encoding.Config
}

// newFrozenChain starts at app version 10, before the rules activate.
func newFrozenChain(t *testing.T, accounts ...string) *frozenChain {
	cparams := app.DefaultConsensusParams()
	cparams.Version.App = frozen.ActivationVersion - 1
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(cparams, accounts...)
	return &frozenChain{app: testApp, kr: kr, enc: encoding.MakeConfig(app.ModuleEncodingRegisters...)}
}

func (c *frozenChain) addr(name string) sdk.AccAddress { return testfactory.GetAddress(c.kr, name) }

func (c *frozenChain) signTx(t *testing.T, name string, msgs []sdk.Msg, opts ...user.TxOption) []byte {
	t.Helper()
	acc := testutil.DirectQueryAccount(c.app, c.addr(name))
	signer, err := user.NewSigner(c.kr, c.enc.TxConfig, testutil.ChainID, user.NewAccount(name, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(t, err)
	opts = append([]user.TxOption{user.SetGasLimit(400_000), user.SetFee(20_000)}, opts...)
	tx, _, err := signer.CreateTx(msgs, opts...)
	require.NoError(t, err)
	return tx
}

// deliver signs msgs as name and commits them alone in a block.
func (c *frozenChain) deliver(t *testing.T, name string, msgs []sdk.Msg, opts ...user.TxOption) *abci.ExecTxResult {
	t.Helper()
	tx := c.signTx(t, name, msgs, opts...)
	next := testutil.GenesisTime.Add(time.Duration(c.app.LastBlockHeight()+1) * time.Second)
	return commitBlockAt(t, c.app, next, tx).TxResults[0]
}

func (c *frozenChain) ctx() sdk.Context {
	return c.app.NewContext(true).WithBlockHeight(c.app.LastBlockHeight()).WithChainID(testutil.ChainID)
}

// upgradeToV11 sets app version 11 and runs the v11 upgrade handler, as the v10 -> v11 transition does.
func (c *frozenChain) upgradeToV11(t *testing.T) {
	execBlock(t, c.app, func(ctx sdk.Context) {
		cp, err := c.app.ConsensusKeeper.ParamsStore.Get(ctx)
		require.NoError(t, err)
		cp.Version.App = frozen.ActivationVersion
		require.NoError(t, c.app.ConsensusKeeper.ParamsStore.Set(ctx, cp))
		plan := upgradetypes.Plan{Name: fmt.Sprintf("v%d", frozen.ActivationVersion), Height: ctx.BlockHeight()}
		require.NoError(t, c.app.UpgradeKeeper.ApplyUpgrade(ctx.WithConsensusParams(cp), plan))
	})
}

func requireOK(t *testing.T, res *abci.ExecTxResult, name string) {
	t.Helper()
	require.Equal(t, uint32(0), res.Code, "%s: %s", name, res.Log)
}

func requireFrozen(t *testing.T, res *abci.ExecTxResult, name string) {
	t.Helper()
	require.Equal(t, frozenCodespace, res.Codespace, "%s: %s", name, res.Log)
	require.Equal(t, frozen.ErrFrozen.ABCICode(), res.Code, name)
}

func utia(n int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, n)) }

// TestFrozenAccounts runs the v10 -> v11 transition: grants and a withdraw
// address set with the exposed key before v11 stay in state but can't move
// funds anywhere except the whitehat.
func TestFrozenAccounts(t *testing.T) {
	c := newFrozenChain(t, "victim", "victim2", "thief")
	victim, victim2, thief := c.addr("victim"), c.addr("victim2"), c.addr("thief")
	whitehat := sdk.MustAccAddressFromBech32(frozen.MainnetWhitehat)
	c.app.FrozenRules.SetNetwork(testutil.ChainID, frozen.MainnetWhitehat, victim, victim2)
	vals, err := c.app.StakingKeeper.GetAllValidators(c.ctx())
	require.NoError(t, err)
	val := vals[0].OperatorAddress
	tia := sdk.NewInt64Coin(appconsts.BondDenom, 1_000_000)
	exp := testutil.GenesisTime.Add(365 * 24 * time.Hour)
	genericGrant := func(msg sdk.Msg) *authz.MsgGrant {
		g, err := authz.NewMsgGrant(victim, thief, authz.NewGenericAuthorization(sdk.MsgTypeURL(msg)), nil)
		require.NoError(t, err)
		return g
	}

	// v10: the rules are inactive, so the exposed key can set up grants and move funds.
	sendAuth, err := authz.NewMsgGrant(victim, thief, banktypes.NewSendAuthorization(utia(100_000_000), nil), &exp)
	require.NoError(t, err)
	allowance, err := feegrant.NewMsgGrantAllowance(&feegrant.BasicAllowance{}, victim, thief)
	require.NoError(t, err)
	for name, msg := range map[string]sdk.Msg{
		"delegate":          stakingtypes.NewMsgDelegate(victim.String(), val, tia.Add(tia)),
		"authz send grant":  sendAuth,
		"authz delegate":    genericGrant(&stakingtypes.MsgDelegate{}),
		"authz withdraw":    genericGrant(&distrtypes.MsgWithdrawDelegatorReward{}),
		"fee allowance":     allowance,
		"withdraw to thief": distrtypes.NewMsgSetWithdrawAddress(victim, thief),
		"send to thief":     banktypes.NewMsgSend(victim, thief, utia(1)),
	} {
		requireOK(t, c.deliver(t, "victim", []sdk.Msg{msg}), name)
	}
	requireOK(t, c.deliver(t, "victim2", []sdk.Msg{stakingtypes.NewMsgDelegate(victim2.String(), val, tia)}), "victim2 delegate")

	c.upgradeToV11(t)

	// No migration: everything set up before v11 is still in state.
	grants, err := c.app.AuthzKeeper.GetAuthorizations(c.ctx(), thief, victim)
	require.NoError(t, err)
	require.Len(t, grants, 3)
	withdrawAddr, err := c.app.DistrKeeper.GetDelegatorWithdrawAddr(c.ctx(), victim)
	require.NoError(t, err)
	require.Equal(t, thief, withdrawAddr)

	// ...but none of it can move funds.
	exec := func(msg sdk.Msg) []sdk.Msg { return []sdk.Msg{new(authz.NewMsgExec(thief, []sdk.Msg{msg}))} }
	withdraw := distrtypes.NewMsgWithdrawDelegatorReward(victim.String(), val)
	requireFrozen(t, c.deliver(t, "thief", exec(banktypes.NewMsgSend(victim, thief, utia(1)))), "MsgExec MsgSend to thief")
	requireFrozen(t, c.deliver(t, "thief", exec(stakingtypes.NewMsgDelegate(victim.String(), val, tia))), "MsgExec MsgDelegate")
	requireFrozen(t, c.deliver(t, "thief", exec(withdraw)), "MsgExec withdraw to thief")
	requireFrozen(t, c.deliver(t, "victim", []sdk.Msg{withdraw}), "withdraw to thief")
	requireFrozen(t, c.deliver(t, "thief", []sdk.Msg{banktypes.NewMsgSend(thief, thief, utia(1))}, user.SetFeeGranter(victim)), "fee granter")

	warpToken := createWarpToken(t, c, thief)
	blocked := map[string]sdk.Msg{
		"send to thief":     banktypes.NewMsgSend(victim, thief, utia(1)),
		"multisend":         banktypes.NewMsgMultiSend(banktypes.NewInput(victim, utia(1)), []banktypes.Output{banktypes.NewOutput(whitehat, utia(1))}),
		"delegate":          stakingtypes.NewMsgDelegate(victim.String(), val, tia),
		"redelegate":        stakingtypes.NewMsgBeginRedelegate(victim.String(), val, val, tia),
		"withdraw to thief": distrtypes.NewMsgSetWithdrawAddress(victim, thief),
		"authz grant":       sendAuth,
		"fee allowance":     allowance,
		"ibc transfer":      ibctransfertypes.NewMsgTransfer("transfer", "channel-0", tia, victim.String(), thief.String(), clienttypes.ZeroHeight(), uint64(exp.UnixNano()), ""),
		"warp transfer":     &warptypes.MsgRemoteTransfer{Sender: victim.String(), TokenId: warpToken, DestinationDomain: 2, Amount: tia.Amount, GasLimit: sdkmath.ZeroInt(), MaxFee: sdk.NewInt64Coin(appconsts.BondDenom, 0)},
		"gov deposit":       govv1.NewMsgDeposit(victim, 1, utia(1)),
		"fibre escrow":      &fibretypes.MsgDepositToEscrow{Signer: victim.String(), Amount: tia},
	}
	for name, msg := range blocked {
		requireFrozen(t, c.deliver(t, "victim", []sdk.Msg{msg}), name)
	}
	requireFrozen(t, c.deliver(t, "victim", []sdk.Msg{banktypes.NewMsgSend(victim, whitehat, utia(1))}, user.SetFee(1_000_001)), "fee above cap")

	// CheckTx and so PrepareProposal reject the same txs.
	checkRes, err := c.app.CheckTx(&abci.RequestCheckTx{Tx: c.signTx(t, "victim", []sdk.Msg{blocked["delegate"]}), Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, frozenCodespace, checkRes.Codespace, checkRes.Log)

	// Allowed: move funds to the whitehat, withdraw rewards to self or the whitehat, unbond.
	before := c.app.BankKeeper.GetBalance(c.ctx(), whitehat, appconsts.BondDenom)
	requireOK(t, c.deliver(t, "victim", []sdk.Msg{banktypes.NewMsgSend(victim, whitehat, utia(5_000))}, user.SetFee(1_000_000)), "send to whitehat")
	require.Equal(t, before.Amount.AddRaw(5_000), c.app.BankKeeper.GetBalance(c.ctx(), whitehat, appconsts.BondDenom).Amount)
	requireOK(t, c.deliver(t, "victim2", []sdk.Msg{distrtypes.NewMsgWithdrawDelegatorReward(victim2.String(), val)}), "withdraw to self")
	requireOK(t, c.deliver(t, "victim", []sdk.Msg{distrtypes.NewMsgSetWithdrawAddress(victim, whitehat)}), "withdraw address to whitehat")
	requireOK(t, c.deliver(t, "victim", []sdk.Msg{withdraw}), "withdraw to whitehat")
	requireOK(t, c.deliver(t, "thief", exec(withdraw)), "MsgExec withdraw to whitehat")
	requireOK(t, c.deliver(t, "victim", []sdk.Msg{stakingtypes.NewMsgUndelegate(victim.String(), val, tia)}), "undelegate")
	valAddr, err := sdk.ValAddressFromBech32(val)
	require.NoError(t, err)
	ubd, err := c.app.StakingKeeper.GetUnbondingDelegation(c.ctx(), victim, valAddr)
	require.NoError(t, err)
	entry := ubd.Entries[0]
	// Unbondings must run to completion, so cancelling one is not allowed.
	requireFrozen(t, c.deliver(t, "victim", []sdk.Msg{stakingtypes.NewMsgCancelUnbondingDelegation(victim.String(), val, entry.CreationHeight, sdk.NewCoin(appconsts.BondDenom, entry.Balance))}), "cancel unbonding")

	// Accounts that aren't listed are unaffected.
	requireOK(t, c.deliver(t, "thief", []sdk.Msg{stakingtypes.NewMsgDelegate(thief.String(), val, tia)}), "unlisted delegate")
}

// TestFrozenBankRoutes checks that the send restriction catches every
// account-to-module route, not only those the ante allow-list rejects.
func TestFrozenBankRoutes(t *testing.T) {
	c := newFrozenChain(t, "victim", "thief")
	victim, thief := c.addr("victim"), c.addr("thief")
	whitehat := sdk.MustAccAddressFromBech32(frozen.MainnetWhitehat)
	c.app.FrozenRules.SetNetwork(testutil.ChainID, frozen.MainnetWhitehat, victim)
	warpToken := createWarpToken(t, c, thief)
	tia := utia(1_000_000)

	var igp pdtypes.MsgCreateIgpResponse
	execBlock(t, c.app, func(ctx sdk.Context) {
		runMsg(t, c.app, ctx, &pdtypes.MsgCreateIgp{Owner: thief.String(), Denom: appconsts.BondDenom}, &igp)
		runMsg(t, c.app, ctx, &pdtypes.MsgSetDestinationGasConfig{Owner: thief.String(), IgpId: igp.Id, DestinationGasConfig: &pdtypes.DestinationGasConfig{
			RemoteDomain: 2, GasOracle: &pdtypes.GasOracle{TokenExchangeRate: sdkmath.NewInt(1e10), GasPrice: sdkmath.NewInt(1)}, GasOverhead: sdkmath.ZeroInt(),
		}}, &pdtypes.MsgSetDestinationGasConfigResponse{})
	})

	ctx := c.ctx()
	vals, err := c.app.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	routes := map[string]sdk.Msg{
		"MsgSend":               &banktypes.MsgSend{FromAddress: victim.String(), ToAddress: thief.String(), Amount: tia},
		"MsgMultiSend":          banktypes.NewMsgMultiSend(banktypes.NewInput(victim, tia), []banktypes.Output{banktypes.NewOutput(thief, tia)}),
		"MsgCreateVestingAcct":  &vestingtypes.MsgCreateVestingAccount{FromAddress: victim.String(), ToAddress: sdk.AccAddress("fresh_thief_address_").String(), Amount: tia, StartTime: 1_600_000_000, EndTime: 2e9},
		"MsgDepositRewardsPool": &distrtypes.MsgDepositValidatorRewardsPool{Depositor: victim.String(), ValidatorAddress: vals[0].OperatorAddress, Amount: tia},
		"MsgFundCommunityPool":  &distrtypes.MsgFundCommunityPool{Depositor: victim.String(), Amount: tia},
		"MsgPayForGas":          &pdtypes.MsgPayForGas{Sender: victim.String(), IgpId: igp.Id, DestinationDomain: 2, GasLimit: sdkmath.NewInt(1), Amount: tia[0]},
		"MsgRemoteTransfer":     &warptypes.MsgRemoteTransfer{Sender: victim.String(), TokenId: warpToken, DestinationDomain: 2, Amount: tia[0].Amount, GasLimit: sdkmath.ZeroInt(), MaxFee: sdk.NewInt64Coin(appconsts.BondDenom, 0)},
		"MsgDepositToEscrow":    &fibretypes.MsgDepositToEscrow{Signer: victim.String(), Amount: tia[0]},
	}
	v10, v11 := withAppVersion(ctx, frozen.ActivationVersion-1), withAppVersion(ctx, frozen.ActivationVersion)
	for name, msg := range routes {
		cacheCtx, _ := v11.CacheContext()
		_, err := c.app.MsgServiceRouter().Handler(msg)(cacheCtx, msg)
		require.ErrorIs(t, err, frozen.ErrFrozen, name)
	}
	// The same send works before v11, on a chain without this list, and to the whitehat.
	send := &banktypes.MsgSend{FromAddress: victim.String(), ToAddress: thief.String(), Amount: tia}
	_, err = c.app.MsgServiceRouter().Handler(send)(v10, send)
	require.NoError(t, err)
	_, err = c.app.MsgServiceRouter().Handler(send)(v11.WithChainID(appconsts.MainnetChainID), send)
	require.NoError(t, err)
	send.ToAddress = whitehat.String()
	_, err = c.app.MsgServiceRouter().Handler(send)(v11, send)
	require.NoError(t, err)
}

// TestFrozenFibreEscrow checks validators stop approving promises from a
// listed escrow once v11 is active, since they could never be settled.
func TestFrozenFibreEscrow(t *testing.T) {
	c := newFrozenChain(t)
	listedKey, unlistedKey := secp256k1.GenPrivKey(), secp256k1.GenPrivKey()
	listed := sdk.AccAddress(listedKey.PubKey().Address())
	c.app.FrozenRules.SetNetwork("test-chain", frozen.MainnetWhitehat, listed)

	validate := func(key *secp256k1.PrivKey) error {
		ctx := c.ctx().WithChainID("test-chain").WithBlockTime(testutil.GenesisTime.Add(time.Hour))
		pub := key.PubKey().(*secp256k1.PubKey)
		deposit := sdk.NewInt64Coin(appconsts.BondDenom, 5_000_000)
		c.app.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: sdk.AccAddress(pub.Address()).String(), Balance: deposit, AvailableBalance: deposit})
		promise := buildSignedPromise(t, ctx.BlockHeight(), ctx.BlockTime().Add(-time.Minute), *pub, key)
		_, err := c.app.FibreKeeper.ValidatePaymentPromise(ctx, &fibretypes.QueryValidatePaymentPromiseRequest{Promise: promise})
		return err
	}
	require.NoError(t, validate(listedKey), "before v11")
	c.upgradeToV11(t)
	err := validate(listedKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), frozen.ErrFrozen.Error())
	require.NoError(t, validate(unlistedKey), "unlisted escrow")
}

func withAppVersion(ctx sdk.Context, v uint64) sdk.Context {
	cp := ctx.ConsensusParams()
	cp.Version = &cmtproto.VersionParams{App: v}
	return ctx.WithConsensusParams(cp)
}

func runMsg(t *testing.T, a *app.App, ctx sdk.Context, msg sdk.Msg, resp proto.Message) {
	t.Helper()
	res, err := a.MsgServiceRouter().Handler(msg)(ctx, msg)
	require.NoError(t, err)
	require.NoError(t, proto.Unmarshal(res.MsgResponses[0].Value, resp))
}

// createWarpToken creates a utia collateral token on a fresh mailbox.
func createWarpToken(t *testing.T, c *frozenChain, owner sdk.AccAddress) (id util.HexAddress) {
	execBlock(t, c.app, func(ctx sdk.Context) {
		var ism ismtypes.MsgCreateNoopIsmResponse
		runMsg(t, c.app, ctx, &ismtypes.MsgCreateNoopIsm{Creator: owner.String()}, &ism)
		var hook pdtypes.MsgCreateNoopHookResponse
		runMsg(t, c.app, ctx, &pdtypes.MsgCreateNoopHook{Owner: owner.String()}, &hook)
		var mailbox coretypes.MsgCreateMailboxResponse
		runMsg(t, c.app, ctx, &coretypes.MsgCreateMailbox{Owner: owner.String(), LocalDomain: 1, DefaultIsm: ism.Id, DefaultHook: &hook.Id, RequiredHook: &hook.Id}, &mailbox)
		var token warptypes.MsgCreateCollateralTokenResponse
		runMsg(t, c.app, ctx, &warptypes.MsgCreateCollateralToken{Owner: owner.String(), OriginMailbox: mailbox.Id, OriginDenom: appconsts.BondDenom}, &token)
		runMsg(t, c.app, ctx, &warptypes.MsgEnrollRemoteRouter{Owner: owner.String(), TokenId: token.Id, RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: 2, ReceiverContract: token.Id, Gas: sdkmath.ZeroInt()}}, &warptypes.MsgEnrollRemoteRouterResponse{})
		id = token.Id
	})
	return id
}
