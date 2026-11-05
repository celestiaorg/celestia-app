package docker_e2e

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/metadata"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

// TestExportRestart halts the chain, exports it and starts it again from the
// export with the same chain-id, the way a mainnet recovery would.
func (s *CelestiaTestSuite) TestExportRestart() {
	s.runExportRestart(false)
}

// TestExportRestartNewChainID is TestExportRestart with a new chain-id.
func (s *CelestiaTestSuite) TestExportRestartNewChainID() {
	s.runExportRestart(true)
}

// runExportRestart checks that state set by transactions survives a full stop
// of the chain, an export and a restart from that export, that the new chain
// starts at the expected height with the exported supply, and that it takes
// new transactions.
func (s *CelestiaTestSuite) runExportRestart(newChainID bool) {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping export restart test in short mode")
	}
	ctx := context.Background()
	suiteName := strings.ReplaceAll(t.Name(), "/", "_")
	s.removeLeftovers(ctx, suiteName+"-", "-"+suiteName)

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	chain, err := dockerchain.NewCelestiaChainBuilder(t, cfg).Build(ctx)
	s.Require().NoError(err, "failed to build the chain")
	t.Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})
	s.Require().NoError(chain.Start(ctx), "failed to start the chain")

	accounts := s.loadExportState(ctx, chain)
	before := s.exportedState(ctx, chain, accounts)

	r := &exportRestart{}
	if newChainID {
		r.newChainID = chain.GetChainID() + "-restart"
	}
	s.exportAndRestart(ctx, chain, r)

	s.Require().Equal(before, s.exportedState(ctx, chain, accounts), "state changed across the restart")
	s.requireFirstBlockSupply(ctx, chain, r)

	s.Run("new transaction", func() {
		resp, err := chain.BroadcastMessages(ctx, chain.GetFaucetWallet(),
			banktypes.NewMsgSend(accounts.faucet, accounts.recipient, sdk.NewCoins(sdk.NewInt64Coin(chain.Config.Denom, 1000))))
		s.Require().NoError(err, "send on the new chain failed")
		s.Require().Equal(uint32(0), resp.Code, "send on the new chain failed: %s", resp.RawLog)
	})
	s.Run("liveness", func() {
		s.Require().NoError(s.CheckLivenessFrom(ctx, chain, r.initialHeight), "validator liveness check failed on the new chain")
	})
}

// exportCodec returns the codec of the Celestia app, which reads its state.
func exportCodec() codec.Codec {
	return encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
}

// exportAccounts are the accounts whose state the export restart test compares.
type exportAccounts struct {
	faucet    sdk.AccAddress
	recipient sdk.AccAddress
	grantee   sdk.AccAddress
	validator sdk.ValAddress
}

// loadExportState sets state that only transactions create: a send, a
// delegation, an unbonding, a fee grant and an authz grant.
func (s *CelestiaTestSuite) loadExportState(ctx context.Context, chain *cosmos.Chain) exportAccounts {
	conn := chain.GetNode().GrpcConn
	faucetWallet := chain.GetFaucetWallet()
	faucet, err := sdk.AccAddressFromBech32(faucetWallet.GetFormattedAddress())
	s.Require().NoError(err)
	accounts := exportAccounts{
		faucet:    faucet,
		recipient: sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()),
		grantee:   sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()),
	}
	validators, err := stakingtypes.NewQueryClient(conn).Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	s.Require().NoError(err)
	s.Require().NotEmpty(validators.Validators)
	accounts.validator, err = sdk.ValAddressFromBech32(validators.Validators[0].OperatorAddress)
	s.Require().NoError(err)

	denom := chain.Config.Denom
	coin := func(amount int64) sdk.Coin { return sdk.NewInt64Coin(denom, amount) }
	allowance, err := feegrant.NewGrant(faucet, accounts.grantee, &feegrant.BasicAllowance{SpendLimit: sdk.NewCoins(coin(3_000_000))})
	s.Require().NoError(err)
	expiration := time.Now().Add(24 * time.Hour)
	authzGrant, err := authz.NewMsgGrant(faucet, accounts.grantee, banktypes.NewSendAuthorization(sdk.NewCoins(coin(4_000_000)), nil), &expiration)
	s.Require().NoError(err)
	msgs := []sdk.Msg{
		banktypes.NewMsgSend(faucet, accounts.recipient, sdk.NewCoins(coin(5_000_000))),
		banktypes.NewMsgSend(faucet, accounts.grantee, sdk.NewCoins(coin(1_000_000))),
		stakingtypes.NewMsgDelegate(faucet.String(), accounts.validator.String(), coin(7_000_000)),
		stakingtypes.NewMsgUndelegate(faucet.String(), accounts.validator.String(), coin(2_000_000)),
		&feegrant.MsgGrantAllowance{Granter: allowance.Granter, Grantee: allowance.Grantee, Allowance: allowance.Allowance},
		authzGrant,
	}
	for _, msg := range msgs {
		resp, err := chain.BroadcastMessages(ctx, faucetWallet, msg)
		s.Require().NoError(err, "failed to broadcast %T", msg)
		s.Require().Equal(uint32(0), resp.Code, "%T failed: %s", msg, resp.RawLog)
	}
	return accounts
}

// exportedState returns the state loadExportState set. Rewards and inflation
// change every block, so only state that no block changes on its own is read.
func (s *CelestiaTestSuite) exportedState(ctx context.Context, chain *cosmos.Chain, a exportAccounts) map[string]string {
	conn := chain.GetNode().GrpcConn
	cdc := exportCodec()
	marshal := func(v proto.Message) string {
		bz, err := cdc.MarshalJSON(v)
		s.Require().NoError(err)
		return string(bz)
	}
	bank := banktypes.NewQueryClient(conn)
	balance := func(addr sdk.AccAddress) string {
		resp, err := bank.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{Address: addr.String()})
		s.Require().NoError(err, "failed to query the balance of %s", addr)
		return resp.Balances.String()
	}
	staking := stakingtypes.NewQueryClient(conn)
	delegation, err := staking.Delegation(ctx, &stakingtypes.QueryDelegationRequest{DelegatorAddr: a.faucet.String(), ValidatorAddr: a.validator.String()})
	s.Require().NoError(err, "failed to query the delegation")
	unbonding, err := staking.UnbondingDelegation(ctx, &stakingtypes.QueryUnbondingDelegationRequest{DelegatorAddr: a.faucet.String(), ValidatorAddr: a.validator.String()})
	s.Require().NoError(err, "failed to query the unbonding delegation")
	validators, err := staking.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	s.Require().NoError(err)
	vals := make([]string, 0, len(validators.Validators))
	for _, v := range validators.Validators {
		vals = append(vals, v.OperatorAddress+" "+v.Tokens.String()+" "+v.Status.String())
	}
	sort.Strings(vals)
	allowance, err := feegrant.NewQueryClient(conn).Allowance(ctx, &feegrant.QueryAllowanceRequest{Granter: a.faucet.String(), Grantee: a.grantee.String()})
	s.Require().NoError(err, "failed to query the fee grant")
	grants, err := authz.NewQueryClient(conn).Grants(ctx, &authz.QueryGrantsRequest{Granter: a.faucet.String(), Grantee: a.grantee.String()})
	s.Require().NoError(err, "failed to query the authz grants")
	s.Require().Len(grants.Grants, 1)

	return map[string]string{
		"recipient balance": balance(a.recipient),
		"grantee balance":   balance(a.grantee),
		"delegation":        delegation.DelegationResponse.Balance.String(),
		"unbonding":         marshal(&unbonding.Unbond),
		"validators":        strings.Join(vals, "; "),
		"fee grant":         marshal(allowance.Allowance),
		"authz grant":       marshal(grants.Grants[0]),
	}
}

// requireFirstBlockSupply checks that the supply in the first block of the new
// chain equals the exported supply. The first block after an import mints
// nothing, so any difference is state the import lost or added.
func (s *CelestiaTestSuite) requireFirstBlockSupply(ctx context.Context, chain *cosmos.Chain, r *exportRestart) {
	var appState map[string]json.RawMessage
	s.Require().NoError(json.Unmarshal(r.exported.AppState, &appState))
	var bankGenesis banktypes.GenesisState
	exportCodec().MustUnmarshalJSON(appState[banktypes.ModuleName], &bankGenesis)
	want := bankGenesis.Supply.AmountOf(chain.Config.Denom)

	heightCtx := metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(r.initialHeight, 10))
	resp, err := banktypes.NewQueryClient(chain.GetNode().GrpcConn).SupplyOf(heightCtx, &banktypes.QuerySupplyOfRequest{Denom: chain.Config.Denom})
	s.Require().NoError(err, "failed to query the supply at height %d", r.initialHeight)
	s.Require().True(want.IsPositive(), "exported supply is empty")
	s.Require().True(want.Equal(resp.Amount.Amount), "supply at the first block %s, exported %s", resp.Amount.Amount, want)
}
