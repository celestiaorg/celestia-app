package hardspoon_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/app/hardspoon"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

const (
	operatorName    = "operator"
	operatorBalance = 20_000_000
	operatorStake   = 10_000_000
	// The fixture's carried network minimum gas price.
	networkMinGasPrice = 0.005
)

// TestGenesisInitChains boots a spooned genesis, with a gentx, through InitChain.
//
// This is the check that matters most, because several of the invariants
// hardspoon maintains are enforced by panics deep inside module InitGenesis
// rather than by anything it can inspect itself:
//
//   - bank panics if the stated supply differs from the sum of balances, which is
//     why the supply is recomputed rather than carried;
//   - staking panics if a pool module account's balance differs from the tokens
//     derived from its validators and delegations, which is why the pool balances
//     have to be dropped along with the delegations;
//   - minfee's ValidateGenesis rejects a zero network_min_gas_price and its
//     InitGenesis reads only params, which is why both fields are written.
//
// A genesis that fails here could never start a chain, and no amount of reading
// the file would say so.
//
// The gentx is required rather than incidental: InitChainer refuses a genesis
// whose validator set is empty after InitGenesis. The published pre-genesis
// therefore cannot be booted on its own by design, only the final genesis with
// gentxs collected into it.
func TestGenesisInitChains(t *testing.T) {
	capp := testApp(t)
	f := newFixture()
	kr := f.withOperator(t, operatorName, operatorBalance)
	opts := defaultOptions()

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.NoError(t, err)

	appState := withGentx(t, result, gentx(t, capp.AppCodec(), kr, operatorName, opts.ChainID, operatorStake, networkMinGasPrice))
	consensusParams := result.Genesis.ConsensusParams.ToProto()

	target := testAppWithChainID(t, opts.ChainID)
	response, err := target.InitChain(&abci.RequestInitChain{
		ChainId:         opts.ChainID,
		Time:            opts.GenesisTime,
		InitialHeight:   opts.InitialHeight,
		ConsensusParams: &consensusParams,
		AppStateBytes:   appState,
	})
	require.NoError(t, err)
	require.Len(t, response.Validators, 1, "the gentx should have created exactly one validator")

	// InitChain leaves its writes in the finalize-block state, uncommitted, so
	// read them through that rather than through the committed store.
	ctx := target.NewContextLegacy(false, cmtproto.Header{Height: opts.InitialHeight})

	t.Run("returned stake is spendable", func(t *testing.T) {
		// 1000 held, plus 10000 of delegation principal and 300 of unbonding.
		require.Equal(t, "11300utia",
			target.BankKeeper.GetBalance(ctx, mustAddress(t, f.Delegator1), denom).String())

		// Held nothing in the export; everything it has is returned stake.
		require.Equal(t, "20000utia",
			target.BankKeeper.GetBalance(ctx, mustAddress(t, f.Delegator2), denom).String())
	})

	t.Run("carried parameters are live", func(t *testing.T) {
		staking, err := target.StakingKeeper.GetParams(ctx)
		require.NoError(t, err)
		require.Equal(t, f.Staking.Params.BondDenom, staking.BondDenom)
		require.Equal(t, f.Staking.Params.UnbondingTime, staking.UnbondingTime)

		// The export only fills minfee's deprecated field, so this is what
		// proves the backfill took effect rather than the default being used.
		require.Equal(t, f.MinFee.NetworkMinGasPrice.String(),
			target.MinFeeKeeper.GetParams(ctx).NetworkMinGasPrice.String())
	})

	t.Run("only the gentx validator is bonded", func(t *testing.T) {
		validators, err := target.StakingKeeper.GetAllValidators(ctx)
		require.NoError(t, err)
		require.Len(t, validators, 1)
		require.Equal(t, sdk.NewInt64Coin(denom, operatorStake).Amount, validators[0].Tokens)
	})

	t.Run("vesting schedule is enforced", func(t *testing.T) {
		// The account holds nothing and its whole original vesting amount is
		// still locked, so clearing delegation tracking must not have made any
		// of it spendable.
		spendable := target.BankKeeper.SpendableCoins(ctx, mustAddress(t, f.Vesting))
		require.True(t, spendable.IsZero(), "spendable was %s", spendable)
	})
}

// TestGentxWithoutFeeIsRejected pins down that a gentx has to pay the network
// minimum gas price.
//
// Nothing in the ante chain is skipped at InitChain: gentxs are delivered through
// the full handler, and the network minimum gas price check in ValidateTxFee sits
// outside the CheckTx-only guard. A gentx built with default flags and no
// explicit fee is therefore rejected, and takes the whole InitChain down with it,
// so validator instructions have to ask for a fee.
func TestGentxWithoutFeeIsRejected(t *testing.T) {
	capp := testApp(t)
	f := newFixture()
	kr := f.withOperator(t, operatorName, operatorBalance)
	opts := defaultOptions()

	result, err := hardspoon.Transform(capp.AppCodec(), capp.DefaultGenesis(), f.fork(t, capp), opts)
	require.NoError(t, err)

	// A gas price of 0 means a zero fee, which is what `genesis gentx` produces
	// unless --fees is passed.
	appState := withGentx(t, result, gentx(t, capp.AppCodec(), kr, operatorName, opts.ChainID, operatorStake, 0))
	consensusParams := result.Genesis.ConsensusParams.ToProto()

	target := testAppWithChainID(t, opts.ChainID)

	// genutil returns an error, which InitChainer turns into a panic, so a bad
	// gentx does not fail gracefully: it takes the node down at startup.
	defer func() {
		recovered := recover()
		require.NotNil(t, recovered, "a zero-fee gentx must not be accepted")
		require.Contains(t, fmt.Sprint(recovered), "insufficient gas price for the network")
		require.Contains(t, fmt.Sprint(recovered), "required: 1000utia")
	}()

	_, _ = target.InitChain(&abci.RequestInitChain{
		ChainId:         opts.ChainID,
		Time:            opts.GenesisTime,
		InitialHeight:   opts.InitialHeight,
		ConsensusParams: &consensusParams,
		AppStateBytes:   appState,
	})
}

// withGentx splices a genutil genesis state into a spooned app state.
func withGentx(t *testing.T, result *hardspoon.Result, genutil json.RawMessage) json.RawMessage {
	t.Helper()

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result.Genesis.AppState, &appState))
	appState[genutiltypes.ModuleName] = genutil

	spliced, err := json.Marshal(appState)
	require.NoError(t, err)
	return spliced
}

func mustAddress(t *testing.T, bech32 string) sdk.AccAddress {
	t.Helper()
	address, err := sdk.AccAddressFromBech32(bech32)
	require.NoError(t, err)
	return address
}
