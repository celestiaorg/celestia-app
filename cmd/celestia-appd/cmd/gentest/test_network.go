package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

// GenTxTest creates a testnet genesis using an existing IRL genesis with its
// corresponging gentxs. It then creates a transaction that delegates to all
// validators in the genesis. This is meant to test that 1) the function to
// create a delegate transaction before mainnet launch works and 2) that the
// genesis validators are correctly loaded from the genesis file. It is meant to
// be ran in CI per PR that adds a new genesis validator.
func GenTxTest(tempDir string, existingGenesis *coretypes.GenesisDoc, genTxs []sdk.Tx) error {
	ecfg := encoding.MakeConfig(app.ModuleBasics)

	// create the genesis file
	accounts := make([]string, 1)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = tmrand.Str(9)
	}
	testAccounts := genesis.NewAccounts(999999999999999999, accounts...)
	// this validator has enough stake to overwrite all other validators
	validator := genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)
	// 2B utia guarantees that our validator will have enough power to take over
	// the network while not being too much to reduce the validators with single
	// digit TIA to 0 resulting in a consensus failure.
	validator.Stake = 2_000_000_000_000_000

	// this will load a testnet genesis from an existing genesis file.
	g, err := genesis.FromDocument(existingGenesis)
	if err != nil {
		return err
	}

	g = g.WithAccounts(testAccounts...).
		WithValidators(validator).
		WithGenTx(genTxs...)

	gDoc, err := g.Export()
	if err != nil {
		return err
	}

	vals, err := ReadGenesisValidators(ecfg, gDoc)
	if err != nil {
		return err
	}

	delegatorAddr := testfactory.GetAddress(g.Keyring(), accounts[0])

	delegateTxBuilder, err := CreateMultiDelegateTx(
		ecfg,
		delegatorAddr,
		vals,
		sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000)),
		// set the gas price and limit to something that will get accepted
		user.SetGasLimitAndFee(10000000, 0.1),
	)
	if err != nil {
		return err
	}

	cfg := testnode.DefaultConfig().WithGenesis(g)

	cctx, cleanup, err := testnode.CINetwork(tempDir, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	// wait for the network to start, with ~300ms blocks, 100 seconds should be plenty
	// even with slow CI and a really large genesis file.
	fmt.Println("Wait up to 100 seconds for the network to start...")
	_, err = cctx.WaitForHeightWithTimeout(2, time.Second*100)
	if err != nil {
		return err
	}

	addr := testfactory.GetAddress(cctx.Keyring, accounts[0])

	signer, err := user.NewSigner(cctx.Keyring, cctx.GRPCClient, addr, ecfg.TxConfig, g.ChainID, 0, 0)
	if err != nil {
		return err
	}

	err = signer.SignTransaction(delegateTxBuilder)
	if err != nil {
		return err
	}

	txBz, err := ecfg.TxConfig.TxEncoder()(delegateTxBuilder.GetTx())
	if err != nil {
		return err
	}

	resp, err := signer.BroadcastTx(cctx.GoContext(), txBz)
	if err != nil {
		return err
	}

	// when this transaction passes, that means that the delegate transaction
	// for all the validators in the genesis was successful, which means that
	// the gentxs were correctly created and are valid. If a single gentx is
	// invalid, the network will not be able to start, and they will not exist
	// in the state.
	if abci.CodeTypeOK != resp.Code {
		return fmt.Errorf("transaction failed with code %d, %v %v %v", resp.Code, resp.Codespace, resp.Logs, resp.RawLog)
	}

	result, err := signer.ConfirmTx(cctx.GoContext(), resp.TxHash)
	if err != nil {
		return err
	}

	// when this transaction passes, that means that the delegate transaction
	// for all the validators in the genesis was successful, which means that
	// the gentxs were correctly created and are valid. If a single gentx is
	// invalid, the network will not be able to start, and they will not exist
	// in the state.
	if abci.CodeTypeOK != result.Code {
		return fmt.Errorf("transaction failed with code %d", result.Code)
	}

	return nil
}

// CreateMultiDelegateTx creates an unsigned json encoded sdk transaction that
// contains a MsgDelegate for each validator in validatorAddrs.
func CreateMultiDelegateTx(
	ecfg encoding.Config,
	delAddr sdk.AccAddress,
	validatorAddrs []sdk.ValAddress,
	amount sdk.Coin,
	opts ...user.TxOption, // use this to specify a gas limit
) (client.TxBuilder, error) {
	msgs := make([]sdk.Msg, len(validatorAddrs))
	for i, valAddr := range validatorAddrs {
		msgDelegate := stakingtypes.NewMsgDelegate(delAddr, valAddr, amount)
		msgs[i] = msgDelegate
	}
	builder := ecfg.TxConfig.NewTxBuilder()
	for _, opt := range opts {
		builder = opt(builder)
	}
	builder.SetMsgs(msgs...)
	return builder, nil
}

// ReadGenesisValidators reads the genesis validators that have included their
// gentx the genesis file.
func ReadGenesisValidators(ecfg encoding.Config, gdoc *coretypes.GenesisDoc) ([]sdk.ValAddress, error) {
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(gdoc.AppState, &appState); err != nil {
		return nil, err
	}

	var genutilState genutiltypes.GenesisState
	rawGenutilState := appState[genutiltypes.ModuleName]
	if err := ecfg.Codec.UnmarshalJSON(rawGenutilState, &genutilState); err != nil {
		return nil, err
	}

	validators := []sdk.ValAddress{}
	for _, tx := range genutilState.GenTxs {
		sdkTx, err := ecfg.TxConfig.TxJSONDecoder()(tx)
		if err != nil {
			return nil, err
		}
		msgs := sdkTx.GetMsgs()
		if len(msgs) != 1 {
			fmt.Printf("skipping genesis transaction with more than one message: %v\n", tx)
			continue
		}
		msg := msgs[0]
		msgCreateVal, ok := msg.(*stakingtypes.MsgCreateValidator)
		if !ok {
			fmt.Printf("skipping genesis transaction that is not a create validator message: %v\n", msg)
			continue
		}
		valAddr, err := sdk.ValAddressFromBech32(msgCreateVal.ValidatorAddress)
		if err != nil {
			return nil, err
		}
		validators = append(validators, valAddr)
	}

	return validators, nil
}
