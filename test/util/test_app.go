package util

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	simulationcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/rs/zerolog"
)

const ChainID = testfactory.ChainID

var (
	GenesisTime   = time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	TestAppLogger = log.NewLogger(
		os.Stdout,
		log.ColorOption(false),
		log.LevelOption(zerolog.WarnLevel),
	)
)

// GenesisValidatorPrivateKey returns the deterministic genesis validator key.
func GenesisValidatorPrivateKey() ed25519.PrivKey {
	return ed25519.GenPrivKeyFromSecret([]byte("celestia-app test genesis validator"))
}

// Get flags every time the simulator is run
func init() {
	simulationcli.GetSimulatorFlags()
}

type EmptyAppOptions struct{}

// Get implements AppOptions
func (ao EmptyAppOptions) Get(_ string) any {
	return nil
}

// SetupTestAppWithGenesisValSet initializes a new app with a validator set and
// genesis accounts that also act as delegators. For simplicity, each validator
// is bonded with a delegation of one consensus engine unit in the default token
// of the app from first genesis account. A no-op logger is set in app.
func SetupTestAppWithGenesisValSet(cparams *tmproto.ConsensusParams, genAccounts ...string) (*app.App, keyring.Keyring) {
	testApp, valSet, kr := NewTestAppWithGenesisSet(cparams, genAccounts...)
	initialiseTestApp(testApp, valSet)
	return testApp, kr
}

func SetupTestAppWithGenesisValSetAndMaxSquareSize(cparams *tmproto.ConsensusParams, maxSquareSize int, genAccounts ...string) (*app.App, keyring.Keyring) {
	testApp, valSet, kr := NewTestAppWithGenesisSetAndMaxSquareSize(cparams, maxSquareSize, genAccounts...)
	initialiseTestApp(testApp, valSet)
	return testApp, kr
}

// SetupTestAppWithoutOptimisticExecution is SetupTestAppWithGenesisValSetAndMaxSquareSize
// with optimistic execution disabled, for ProcessProposal benchmarks.
func SetupTestAppWithoutOptimisticExecution(cparams *tmproto.ConsensusParams, maxSquareSize int, genAccounts ...string) (*app.App, keyring.Keyring) {
	testApp, valSet, kr := newTestAppWithGenesisSetAndMaxSquareSize(NewTestApp(baseapp.DisableOptimisticExecution()), cparams, maxSquareSize, genAccounts...)
	initialiseTestApp(testApp, valSet)
	return testApp, kr
}

func initialiseTestApp(testApp *app.App, valSet *tmtypes.ValidatorSet) {
	// first block
	_, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               GenesisTime,
		Height:             testApp.LastBlockHeight() + 1,
		Hash:               testApp.LastCommitID().Hash,
		NextValidatorsHash: valSet.Hash(),
	})
	if err != nil {
		panic(err)
	}

	_, err = testApp.Commit()
	if err != nil {
		panic(err)
	}
}

// WithBlockHash sets req.Hash to a stand-in for the CometBFT block hash that
// commits to every executed field, so optimistic execution is reused only for the processed block.
func WithBlockHash(req *abci.RequestFinalizeBlock) *abci.RequestFinalizeBlock {
	unhashed := *req
	unhashed.Hash = nil
	bz, err := unhashed.Marshal()
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256(bz)
	req.Hash = hash[:]
	return req
}

// WithProposalBlockHash sets req.Hash to the WithBlockHash of the FinalizeBlock
// request celestia-core sends once this proposal is decided (blob txs stripped).
func WithProposalBlockHash(req *abci.RequestProcessProposal) *abci.RequestProcessProposal {
	req.Hash = WithBlockHash(&abci.RequestFinalizeBlock{
		Txs:                CoreFinalizeTxs(req.Txs),
		DecidedLastCommit:  req.ProposedLastCommit,
		Misbehavior:        req.Misbehavior,
		Height:             req.Height,
		Time:               req.Time,
		NextValidatorsHash: req.NextValidatorsHash,
		ProposerAddress:    req.ProposerAddress,
		Header:             req.Header,
	}).Hash
	return req
}

// CoreFinalizeTxs is the loop celestia-core runs on the block txs before
// FinalizeBlock (state/execution.go applyBlock).
func CoreFinalizeTxs(blockTxs [][]byte) [][]byte {
	txs := make([][]byte, len(blockTxs))
	for i, tx := range blockTxs {
		if blobTx, isBlobTx := tmtypes.UnmarshalBlobTx(tx); isBlobTx {
			tx = blobTx.Tx
		}
		txs[i] = tx
	}
	return txs
}

// NewTestApp creates a new app instance with an empty memDB and a no-op logger.
func NewTestApp(baseAppOptions ...func(*baseapp.BaseApp)) *app.App {
	db := dbm.NewMemDB()

	return app.New(
		TestAppLogger,
		db,
		nil,
		0,
		0,
		EmptyAppOptions{},
		append([]func(*baseapp.BaseApp){baseapp.SetChainID(ChainID)}, baseAppOptions...)...,
	)
}

// NewTestAppWithGenesisSet initializes a new app with genesis accounts and returns the testApp, validator set and keyring.
func NewTestAppWithGenesisSet(cparams *tmproto.ConsensusParams, genAccounts ...string) (*app.App, *tmtypes.ValidatorSet, keyring.Keyring) {
	testApp := NewTestApp()
	genesisState, valSet, kr := GenesisStateWithSingleValidator(testApp, genAccounts...)
	testApp = InitialiseTestAppWithGenesis(testApp, cparams, genesisState)
	return testApp, valSet, kr
}

// NewTestAppWithGenesisSetAndMaxSquareSize initializes a new app with genesis accounts and a specific max square size
// and returns the testApp, validator set and keyring.
func NewTestAppWithGenesisSetAndMaxSquareSize(cparams *tmproto.ConsensusParams, maxSquareSize int, genAccounts ...string) (*app.App, *tmtypes.ValidatorSet, keyring.Keyring) {
	return newTestAppWithGenesisSetAndMaxSquareSize(NewTestApp(), cparams, maxSquareSize, genAccounts...)
}

func newTestAppWithGenesisSetAndMaxSquareSize(testApp *app.App, cparams *tmproto.ConsensusParams, maxSquareSize int, genAccounts ...string) (*app.App, *tmtypes.ValidatorSet, keyring.Keyring) {
	genesisState, valSet, kr := GenesisStateWithSingleValidator(testApp, genAccounts...)

	// hacky way of changing the gov max square size without changing the consts
	blobJSON := string(genesisState["blob"])
	replace := strings.Replace(blobJSON, fmt.Sprintf("%d", appconsts.DefaultGovMaxSquareSize), fmt.Sprintf("%d", maxSquareSize), 1)
	genesisState["blob"] = json.RawMessage(replace)

	testApp = InitialiseTestAppWithGenesis(testApp, cparams, genesisState)
	return testApp, valSet, kr
}

// InitialiseTestAppWithGenesis initializes the provided app with the provided genesis.
func InitialiseTestAppWithGenesis(testApp *app.App, cparams *tmproto.ConsensusParams, genesisState app.GenesisState) *app.App {
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

	_, err = testApp.Info(&abci.RequestInfo{})
	if err != nil {
		panic(err)
	}

	// init chain will set the validator set and initialize the genesis accounts
	cparams.Block.MaxBytes = int64(appconsts.BlockMaxBytes)
	_, err = testApp.InitChain(
		&abci.RequestInitChain{
			Time:            GenesisTime,
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: cparams,
			AppStateBytes:   stateBytes,
			ChainId:         ChainID,
		},
	)
	if err != nil {
		panic(err)
	}

	return testApp
}

// GenesisStateWithSingleValidator initializes GenesisState with a single
// validator and genesis accounts that also act as delegators.
func GenesisStateWithSingleValidator(testApp *app.App, genAccounts ...string) (app.GenesisState, *tmtypes.ValidatorSet, keyring.Keyring) {
	// create validator set with single validator
	validatorPubKey := GenesisValidatorPrivateKey().PubKey()
	validator := tmtypes.NewValidator(validatorPubKey, 1)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})
	genesisState, kr := GenesisStateWithValidatorSet(testApp, valSet, genAccounts...)
	return genesisState, valSet, kr
}

// GenesisStateWithValidatorSet initializes GenesisState with valSet, each
// validator bonding its voting power, and genesis accounts that also act as
// delegators.
func GenesisStateWithValidatorSet(testApp *app.App, valSet *tmtypes.ValidatorSet, genAccounts ...string) (app.GenesisState, keyring.Keyring) {
	// generate sender account
	senderPrivKey := secp256k1.GenPrivKeyFromSecret([]byte("09876543210987654321098765432109"))
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)

	// append sender account to genesis accounts
	accs := make([]authtypes.GenesisAccount, 0, len(genAccounts)+1)
	accs = append(accs, acc)

	// genesis accounts and sender account balances
	balances := make([]banktypes.Balance, 0, len(genAccounts)+1)
	balances = append(balances, banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(100000000000000))),
	})

	kr, fundedBankAccs, fundedAuthAccs := testnode.FundKeyringAccounts(genAccounts...)

	accs = append(accs, fundedAuthAccs...)
	balances = append(balances, fundedBankAccs...)

	genesisState := testApp.DefaultGenesis()
	genesisState = genesisStateWithValSet(testApp, genesisState, valSet, accs, balances...)

	return genesisState, kr
}

func genesisStateWithValSet(
	a *app.App,
	genesisState app.GenesisState,
	valSet *tmtypes.ValidatorSet,
	genAccs []authtypes.GenesisAccount,
	balances ...banktypes.Balance,
) app.GenesisState {
	// set genesis accounts
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	genesisState[authtypes.ModuleName] = a.AppCodec().MustMarshalJSON(authGenesis)

	validators := make([]stakingtypes.Validator, 0, len(valSet.Validators))
	delegations := make([]stakingtypes.Delegation, 0, len(valSet.Validators))

	// Each validator bonds its CometBFT voting power in consensus engine units.
	totalBonded := math.ZeroInt()
	for _, val := range valSet.Validators {
		bondAmt := sdk.TokensFromConsensusPower(val.VotingPower, sdk.DefaultPowerReduction)
		totalBonded = totalBonded.Add(bondAmt)
		pk, err := cryptocodec.FromCmtPubKeyInterface(val.PubKey)
		if err != nil {
			panic(err)
		}
		pkAny, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			panic(err)
		}
		rate, err := math.LegacyNewDecFromStr("0.05")
		if err != nil {
			panic(err)
		}
		maxRate, err := math.LegacyNewDecFromStr("0.2")
		if err != nil {
			panic(err)
		}
		maxChangeRate, err := math.LegacyNewDecFromStr("1")
		if err != nil {
			panic(err)
		}
		validator := stakingtypes.Validator{
			OperatorAddress:   sdk.ValAddress(val.Address).String(),
			ConsensusPubkey:   pkAny,
			Jailed:            false,
			Status:            stakingtypes.Bonded,
			Tokens:            bondAmt,
			DelegatorShares:   math.LegacyOneDec(),
			Description:       stakingtypes.Description{},
			UnbondingHeight:   int64(0),
			UnbondingTime:     time.Unix(0, 0).UTC(),
			Commission:        stakingtypes.NewCommission(rate, maxRate, maxChangeRate),
			MinSelfDelegation: math.ZeroInt(),
		}
		validators = append(validators, validator)

		addrCodec := a.AppCodec().InterfaceRegistry().SigningContext().AddressCodec()
		valCodec := a.AppCodec().InterfaceRegistry().SigningContext().ValidatorAddressCodec()
		delegatorAddr, err := addrCodec.BytesToString(genAccs[0].GetAddress())
		if err != nil {
			panic(err)
		}
		valAddr, err := valCodec.BytesToString(val.Address)
		if err != nil {
			panic(err)
		}
		delegations = append(delegations, stakingtypes.NewDelegation(delegatorAddr, valAddr, math.LegacyOneDec()))
	}
	// set validators and delegations
	params := stakingtypes.DefaultParams()
	params.BondDenom = appconsts.BondDenom
	stakingGenesis := stakingtypes.NewGenesisState(params, validators, delegations)
	genesisState[stakingtypes.ModuleName] = a.AppCodec().MustMarshalJSON(stakingGenesis)

	totalSupply := sdk.NewCoins()
	for _, b := range balances {
		// add genesis acc tokens to total supply
		totalSupply = totalSupply.Add(b.Coins...)
	}

	// add delegated tokens to total supply
	totalSupply = totalSupply.Add(sdk.NewCoin(params.BondDenom, totalBonded))

	// add bonded amount to bonded pool module account
	balances = append(balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.Coins{sdk.NewCoin(params.BondDenom, totalBonded)},
	})

	// update total supply
	bankGenesis := banktypes.NewGenesisState(banktypes.DefaultGenesisState().Params, balances, totalSupply, []banktypes.Metadata{}, []banktypes.SendEnabled{})
	genesisState[banktypes.ModuleName] = a.AppCodec().MustMarshalJSON(bankGenesis)

	return genesisState
}
