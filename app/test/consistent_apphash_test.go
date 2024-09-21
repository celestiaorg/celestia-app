package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	signal "github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisisTypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

type blobTx struct {
	author    string
	blobs     []*share.Blob
	txOptions []user.TxOption
}

type (
	encodedSdkMessages func(*testing.T, []sdk.AccAddress, []stakingtypes.Validator, *app.App, *user.Signer, *user.Signer) ([][]byte, [][]byte, [][]byte)
	encodedBlobTxs     func(*testing.T, *user.Signer, []sdk.AccAddress) []byte
)

type appHashTest struct {
	name               string
	version            uint64
	encodedSdkMessages encodedSdkMessages
	encodedBlobTxs     encodedBlobTxs
	expectedDataRoot   []byte
	expectedAppHash    []byte
}

// TestConsistentAppHash executes all state machine messages on all app versions, generates an app hash,
// and compares it against a previously generated hash from the same set of transactions.
// App hashes across different commits should be consistent.
func TestConsistentAppHash(t *testing.T) {
	tc := []appHashTest{
		{
			name:               "execute sdk messages and blob tx on v1",
			version:            v1.Version,
			encodedSdkMessages: encodedSdkMessagesV1,
			encodedBlobTxs:     createEncodedBlobTx,
			expectedDataRoot:   []byte{30, 142, 46, 120, 191, 30, 242, 150, 164, 242, 166, 245, 89, 183, 181, 41, 88, 197, 11, 19, 243, 46, 69, 97, 3, 51, 27, 133, 68, 95, 95, 121},
			// Expected app hash produced by v1.x - https://github.com/celestiaorg/celestia-app/blob/v1.x/app/consistent_apphash_test.go
			expectedAppHash: []byte{57, 128, 107, 57, 6, 131, 221, 188, 181, 181, 135, 58, 37, 240, 135, 66, 199, 107, 80, 154, 240, 176, 57, 36, 238, 69, 25, 188, 86, 203, 145, 145},
		},
		{
			name:    "execute sdk messages and blob tx on v2",
			version: v2.Version,
			encodedSdkMessages: func(t *testing.T, accountAddresses []sdk.AccAddress, genValidators []stakingtypes.Validator, testApp *app.App, signer *user.Signer, valSigner *user.Signer) ([][]byte, [][]byte, [][]byte) {
				firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs := encodedSdkMessagesV1(t, accountAddresses, genValidators, testApp, signer, valSigner)
				encodedMessagesV2 := encodedSdkMessagesV2(t, genValidators, valSigner)
				thirdBlockEncodedTxs = append(thirdBlockEncodedTxs, encodedMessagesV2...)

				return firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs
			},
			encodedBlobTxs:   createEncodedBlobTx,
			expectedDataRoot: []byte{200, 61, 245, 166, 119, 211, 170, 2, 73, 239, 253, 97, 243, 112, 116, 196, 70, 41, 201, 172, 123, 28, 15, 182, 52, 222, 122, 243, 95, 97, 66, 233},
			// Expected app hash produced on v2.x - https://github.com/celestiaorg/celestia-app/blob/v2.x/app/test/consistent_apphash_test.go
			expectedAppHash: []byte{14, 115, 34, 28, 33, 70, 118, 3, 111, 250, 161, 185, 187, 151, 54, 78, 86, 37, 44, 252, 8, 26, 164, 251, 36, 20, 151, 170, 181, 84, 32, 136},
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testutil.NewTestApp()
			enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			// Create deterministic keys
			kr, pubKeys := deterministicKeyRing(enc.Codec)
			consensusParams := app.DefaultConsensusParams()
			consensusParams.Version.AppVersion = tt.version
			// Apply genesis state to the app.
			valKeyRing, _, err := testutil.SetupDeterministicGenesisState(testApp, pubKeys, 20_000_000_000, consensusParams)
			require.NoError(t, err)

			// Get account names and addresses from the keyring and create signer
			signer, accountAddresses := getAccountsAndCreateSigner(t, kr, enc.TxConfig, testutil.ChainID, tt.version, testApp)
			// Validators from genesis state
			genValidators := testApp.StakingKeeper.GetAllValidators(testApp.NewContext(false, tmproto.Header{}))
			valSigner, _ := getAccountsAndCreateSigner(t, valKeyRing, enc.TxConfig, testutil.ChainID, tt.version, testApp)

			// Convert validators to ABCI validators
			abciValidators, err := convertToABCIValidators(genValidators)
			require.NoError(t, err)

			firstBlockTxs, secondBlockTxs, thirdBlockTxs := tt.encodedSdkMessages(t, accountAddresses, genValidators, testApp, signer, valSigner)
			encodedBlobTx := tt.encodedBlobTxs(t, signer, accountAddresses)

			// Execute the first block
			_, firstBlockAppHash, err := executeTxs(testApp, []byte{}, firstBlockTxs, abciValidators, testApp.LastCommitID().Hash)
			require.NoError(t, err)
			// Execute the second block
			_, secondBlockAppHash, err := executeTxs(testApp, encodedBlobTx, secondBlockTxs, abciValidators, firstBlockAppHash)
			require.NoError(t, err)
			// Execute the final block and get the data root alongside the final app hash
			finalDataRoot, finalAppHash, err := executeTxs(testApp, []byte{}, thirdBlockTxs, abciValidators, secondBlockAppHash)
			require.NoError(t, err)

			// Require that the app hash is equal to the app hash produced on a different commit
			require.Equal(t, tt.expectedAppHash, finalAppHash)
			// Require that the data root is equal to the data root produced on a different commit
			require.Equal(t, tt.expectedDataRoot, finalDataRoot)
		})
	}
}

// getAccountsAndCreateSigner returns a signer with accounts
func getAccountsAndCreateSigner(t *testing.T, kr keyring.Keyring, enc client.TxConfig, chainID string, appVersion uint64, testApp *app.App) (*user.Signer, []sdk.AccAddress) {
	// Get account names and addresses from the keyring
	accountNames := testfactory.GetAccountNames(kr)
	accountAddresses := testfactory.GetAddresses(kr)
	// Query keyring account infos
	accountInfos := queryAccountInfo(testApp, accountNames, kr)
	// Create accounts for the signer
	accounts := createAccounts(accountInfos, accountNames)
	// Create a signer with accounts
	signer, err := user.NewSigner(kr, enc, chainID, appVersion, accounts...)
	require.NoError(t, err)
	return signer, accountAddresses
}

// encodedSdkMessagesV1 returns encoded SDK messages for v1
func encodedSdkMessagesV1(t *testing.T, accountAddresses []sdk.AccAddress, genValidators []stakingtypes.Validator, testApp *app.App, signer *user.Signer, valSigner *user.Signer) ([][]byte, [][]byte, [][]byte) {
	// ----------- Create v1 SDK Messages ------------

	amount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1_000)))
	// Minimum deposit required for a gov proposal to become active
	depositAmount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(10000000000)))
	twoInt := sdk.NewInt(2)

	// ---------------- First Block ------------
	var firstBlockSdkMsgs []sdk.Msg

	// NewMsgSend - sends funds from account-0 to account-1
	sendFundsMsg := banktypes.NewMsgSend(accountAddresses[0], accountAddresses[1], amount)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, sendFundsMsg)

	// MultiSend - creates a multi-send transaction from account-0 to account-1
	multiSendFundsMsg := banktypes.NewMsgMultiSend([]banktypes.Input{
		banktypes.NewInput(
			accountAddresses[0],
			amount,
		),
	},
		[]banktypes.Output{
			banktypes.NewOutput(
				accountAddresses[1],
				amount,
			),
		})
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, multiSendFundsMsg)

	// NewMsgGrant - grants authorization to account-1
	grantExpiration := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	authorization := authz.NewGenericAuthorization(blobtypes.URLMsgPayForBlobs)
	msgGrant, err := authz.NewMsgGrant(
		accountAddresses[0],
		accountAddresses[1],
		authorization,
		&grantExpiration,
	)
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgGrant)

	// MsgVerifyInvariant - verifies the nonnegative-outstanding invariant within the bank module for the account-0
	msgVerifyInvariant := crisisTypes.NewMsgVerifyInvariant(accountAddresses[0], banktypes.ModuleName, "nonnegative-outstanding")
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgVerifyInvariant)

	// MsgGrantAllowance - creates a grant allowance for account-1
	basicAllowance := feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000))),
	}
	feegrantMsg, err := feegrant.NewMsgGrantAllowance(&basicAllowance, accountAddresses[0], accountAddresses[1])
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, feegrantMsg)

	// NewMsgSubmitProposal - submits a proposal to send funds from the governance account to account-1
	govAccount := testApp.GovKeeper.GetGovernanceAccount(testApp.NewContext(false, tmproto.Header{})).GetAddress()
	msgSend := banktypes.MsgSend{
		FromAddress: govAccount.String(),
		ToAddress:   accountAddresses[1].String(),
		Amount:      amount,
	}
	proposal, err := govtypes.NewMsgSubmitProposal([]sdk.Msg{&msgSend}, amount, accountAddresses[0].String(), "")
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, proposal)

	// NewMsgDeposit - deposits funds to a governance proposal
	msgDeposit := govtypes.NewMsgDeposit(accountAddresses[0], 1, depositAmount)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgDeposit)

	// NewMsgCreateValidator - creates a new validator
	msgCreateValidator, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(accountAddresses[6]),
		ed25519.GenPrivKeyFromSecret([]byte("validator")).PubKey(),
		amount[0],
		stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
		stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 0o2), sdk.NewDecWithPrec(12, 0o2), sdk.NewDecWithPrec(1, 0o2)),
		sdk.OneInt())
	require.NoError(t, err)
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgCreateValidator)

	// NewMsgDelegate - delegates funds to validator-0
	msgDelegate := stakingtypes.NewMsgDelegate(accountAddresses[0], genValidators[0].GetOperator(), amount[0])
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgDelegate)

	// NewMsgBeginRedelegate - re-delegates funds from validator-0 to validator-1
	msgBeginRedelegate := stakingtypes.NewMsgBeginRedelegate(accountAddresses[0], genValidators[0].GetOperator(), genValidators[1].GetOperator(), amount[0])
	firstBlockSdkMsgs = append(firstBlockSdkMsgs, msgBeginRedelegate)

	// ------------ Second Block ------------

	var secondBlockSdkMsgs []sdk.Msg

	// NewMsgVote - votes yes on a governance proposal
	msgVote := govtypes.NewMsgVote(accountAddresses[0], 1, govtypes.VoteOption_VOTE_OPTION_YES, "")
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgVote)

	// NewMsgRevoke - revokes authorization from account-1
	msgRevoke := authz.NewMsgRevoke(
		accountAddresses[0],
		accountAddresses[1],
		blobtypes.URLMsgPayForBlobs,
	)

	// NewMsgExec - executes the revoke authorization message
	msgExec := authz.NewMsgExec(accountAddresses[0], []sdk.Msg{&msgRevoke})
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, &msgExec)

	// NewMsgVoteWeighted - votes with a weighted vote
	msgVoteWeighted := govtypes.NewMsgVoteWeighted(
		accountAddresses[0],
		1,
		govtypes.WeightedVoteOptions([]*govtypes.WeightedVoteOption{{Option: govtypes.OptionYes, Weight: "1.0"}}), // Cast the slice to the expected type
		"",
	)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgVoteWeighted)

	// NewMsgEditValidator - edits the newly created validator's description
	msgEditValidator := stakingtypes.NewMsgEditValidator(sdk.ValAddress(accountAddresses[6]), stakingtypes.NewDescription("add", "new", "val", "desc", "."), nil, &twoInt)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgEditValidator)

	// NewMsgUndelegate - undelegates funds from validator-1
	msgUndelegate := stakingtypes.NewMsgUndelegate(accountAddresses[0], genValidators[1].GetOperator(), amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgUndelegate)

	// NewMsgDelegate - delegates funds to validator-0
	msgDelegate = stakingtypes.NewMsgDelegate(accountAddresses[0], genValidators[0].GetOperator(), amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgDelegate)

	// Block 2 height
	blockHeight := testApp.LastBlockHeight() + 2
	// NewMsgCancelUnbondingDelegation - cancels unbonding delegation from validator-1
	msgCancelUnbondingDelegation := stakingtypes.NewMsgCancelUnbondingDelegation(accountAddresses[0], genValidators[1].GetOperator(), blockHeight, amount[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgCancelUnbondingDelegation)

	// NewMsgSetWithdrawAddress - sets the withdraw address for account-0
	msgSetWithdrawAddress := distribution.NewMsgSetWithdrawAddress(accountAddresses[0], accountAddresses[1])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgSetWithdrawAddress)

	// NewMsgRevokeAllowance - revokes the allowance granted to account-1
	msgRevokeAllowance := feegrant.NewMsgRevokeAllowance(accountAddresses[0], accountAddresses[1])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, &msgRevokeAllowance)

	// NewMsgFundCommunityPool - funds the community pool
	msgFundCommunityPool := distribution.NewMsgFundCommunityPool(amount, accountAddresses[0])
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgFundCommunityPool)

	// NewMsgWithdrawDelegatorReward - withdraws delegator rewards
	msgWithdrawDelegatorReward := distribution.NewMsgWithdrawDelegatorReward(accountAddresses[0], genValidators[0].GetOperator())
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgWithdrawDelegatorReward)

	// NewMsgCreatePeriodicVestingAccount - creates a periodic vesting account
	newAddress := sdk.AccAddress(ed25519.GenPrivKeyFromSecret([]byte("anotherAddress")).PubKey().Address())
	vestingPeriod := []vestingtypes.Period{
		{
			Length: 3600,
			Amount: amount,
		},
	}
	msgCreatePeriodicVestingAccount := vestingtypes.NewMsgCreatePeriodicVestingAccount(accountAddresses[3], newAddress, 2, vestingPeriod)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgCreatePeriodicVestingAccount)

	// NewMsgCreatePermanentLockedAccount - creates a permanent locked account
	newAddress = sdk.AccAddress(ed25519.GenPrivKeyFromSecret([]byte("anotherAddress2")).PubKey().Address())
	msgCreatePermamentLockedAccount := vestingtypes.NewMsgCreatePermanentLockedAccount(accountAddresses[3], newAddress, amount)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgCreatePermamentLockedAccount)

	// NewMsgCreateVestingAccount - creates a vesting account
	newAddress = sdk.AccAddress(ed25519.GenPrivKeyFromSecret([]byte("anotherAddress3")).PubKey().Address())
	msgCreateVestingAccount := vestingtypes.NewMsgCreateVestingAccount(accountAddresses[3], newAddress, amount, 1, 2, false)
	secondBlockSdkMsgs = append(secondBlockSdkMsgs, msgCreateVestingAccount)

	// ------------ Third Block ------------

	// Txs within the third block are signed by the validator's signer
	var thirdBlockSdkMsgs []sdk.Msg

	// NewMsgWithdrawValidatorCommission - withdraws validator-0's commission
	msgWithdrawValidatorCommission := distribution.NewMsgWithdrawValidatorCommission(genValidators[0].GetOperator())
	thirdBlockSdkMsgs = append(thirdBlockSdkMsgs, msgWithdrawValidatorCommission)

	// NewMsgUnjail - unjails validator-3
	msgUnjail := slashingtypes.NewMsgUnjail(genValidators[3].GetOperator())
	thirdBlockSdkMsgs = append(thirdBlockSdkMsgs, msgUnjail)

	// NewMsgRegisterEVMAddress - registers an EVM address
	// This message was removed in v2
	if testApp.AppVersion() == v1.Version {
		msgRegisterEVMAddress := blobstreamtypes.NewMsgRegisterEVMAddress(genValidators[1].GetOperator(), gethcommon.HexToAddress("hi"))
		thirdBlockSdkMsgs = append(thirdBlockSdkMsgs, msgRegisterEVMAddress)
	}

	firstBlockTxs, err := processSdkMessages(signer, firstBlockSdkMsgs)
	require.NoError(t, err)
	secondBlockTxs, err := processSdkMessages(signer, secondBlockSdkMsgs)
	require.NoError(t, err)
	thirdBlockTxs, err := processSdkMessages(valSigner, thirdBlockSdkMsgs)
	require.NoError(t, err)

	return firstBlockTxs, secondBlockTxs, thirdBlockTxs
}

// encodedSdkMessagesV2 returns encoded SDK messages introduced in v2
func encodedSdkMessagesV2(t *testing.T, genValidators []stakingtypes.Validator, valSigner *user.Signer) [][]byte {
	var v2Messages []sdk.Msg
	msgTryUpgrade := signal.NewMsgTryUpgrade(sdk.AccAddress(genValidators[0].GetOperator()))
	v2Messages = append(v2Messages, msgTryUpgrade)

	msgSignalVersion := signal.NewMsgSignalVersion(genValidators[0].GetOperator(), 2)
	v2Messages = append(v2Messages, msgSignalVersion)

	encodedTxs, err := processSdkMessages(valSigner, v2Messages)
	require.NoError(t, err)

	return encodedTxs
}

// createEncodedBlobTx creates, signs and returns an encoded blob transaction
func createEncodedBlobTx(t *testing.T, signer *user.Signer, accountAddresses []sdk.AccAddress) []byte {
	senderAcc := signer.AccountByAddress(accountAddresses[1])
	blob, err := share.NewBlob(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion, nil)
	require.NoError(t, err)

	// Create a Blob Tx
	blobTx := blobTx{
		author:    senderAcc.Name(),
		blobs:     []*share.Blob{blob},
		txOptions: blobfactory.DefaultTxOpts(),
	}
	encodedBlobTx, _, err := signer.CreatePayForBlobs(blobTx.author, blobTx.blobs, blobTx.txOptions...)
	require.NoError(t, err)
	return encodedBlobTx
}

// fixedNamespace returns a hardcoded namespace
func fixedNamespace() share.Namespace {
	ns, err := share.NewNamespace(0, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 67, 154, 200, 228, 130, 74, 147, 162, 11})
	if err != nil {
		panic(err)
	}
	return ns
}

// deterministicKeyRing returns a deterministic keyring and a list of deterministic public keys
func deterministicKeyRing(cdc codec.Codec) (keyring.Keyring, []types.PubKey) {
	mnemonics := []string{
		"great myself congress genuine scale muscle view uncover pipe miracle sausage broccoli lonely swap table foam brand turtle comic gorilla firm mad grunt hazard",
		"cheap job month trigger flush cactus chest juice dolphin people limit crunch curious secret object beach shield snake hunt group sketch cousin puppy fox",
		"oil suffer bamboo one better attack exist dolphin relief enforce cat asset raccoon lava regret found love certain plunge grocery accuse goat together kiss",
		"giraffe busy subject doll jump drama sea daring again club spend toe mind organ real liar permit refuse change opinion donkey job cricket speed",
		"fee vapor thing fish fan memory negative raven cram win quantum ozone job mirror shoot sting quiz black apart funny sort cancel friend curtain",
		"skin beef review pilot tooth act any alarm there only kick uniform ticket material cereal radar ethics list unlock method coral smooth street frequent",
		"ecology scout core guard load oil school effort near alcohol fancy save cereal owner enforce impact sand husband trophy solve amount fish festival sell",
		"used describe angle twin amateur pyramid bitter pool fluid wing erode rival wife federal curious drink battle put elbow mandate another token reveal tone",
		"reason fork target chimney lift typical fine divorce mixture web robot kiwi traffic stove miss crane welcome camp bless fuel october riot pluck ordinary",
		"undo logic mobile modify master force donor rose crumble forget plate job canal waste turn damp sure point deposit hazard quantum car annual churn",
		"charge subway treat loop donate place loan want grief leg message siren joy road exclude match empty enforce vote meadow enlist vintage wool involve",
	}
	kb := keyring.NewInMemory(cdc)
	pubKeys := make([]types.PubKey, len(mnemonics))
	for idx, mnemonic := range mnemonics {
		rec, err := kb.NewAccount(fmt.Sprintf("account-%d", idx), mnemonic, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
		pubKey, err := rec.GetPubKey()
		if err != nil {
			panic(err)
		}
		pubKeys[idx] = pubKey
	}
	return kb, pubKeys
}

// processSdkMessages takes a list of sdk messages, forms transactions, signs them
// and returns a list of encoded transactions
func processSdkMessages(signer *user.Signer, sdkMessages []sdk.Msg) ([][]byte, error) {
	encodedTxs := make([][]byte, 0, len(sdkMessages))
	for _, msg := range sdkMessages {
		encodedTx, err := signer.CreateTx([]sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
		if err != nil {
			return nil, err
		}

		signerAddress := msg.GetSigners()[0]
		signerAccount := signer.AccountByAddress(signerAddress)
		err = signer.SetSequence(signerAccount.Name(), signerAccount.Sequence()+1)
		if err != nil {
			return nil, err
		}

		encodedTxs = append(encodedTxs, encodedTx)
	}
	return encodedTxs, nil
}

// executeTxs executes a set of transactions and returns the data hash and app hash
func executeTxs(testApp *app.App, encodedBlobTx []byte, encodedSdkTxs [][]byte, validators []abci.Validator, lastCommitHash []byte) ([]byte, []byte, error) {
	height := testApp.LastBlockHeight() + 1
	chainID := testApp.GetChainID()

	genesisTime := testutil.GenesisTime

	// Prepare Proposal
	resPrepareProposal := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: encodedSdkTxs,
		},
		ChainId: chainID,
		Height:  height,
		// Dynamically increase time so the validator can be unjailed (1m duration)
		Time: genesisTime.Add(time.Duration(height) * time.Minute),
	})
	if len(resPrepareProposal.BlockData.Txs) != len(encodedSdkTxs) {
		return nil, nil, fmt.Errorf("PrepareProposal removed transactions. Was %d, now %d", len(encodedSdkTxs), len(resPrepareProposal.BlockData.Txs))
	}

	dataHash := resPrepareProposal.BlockData.Hash

	header := tmproto.Header{
		Version:        version.Consensus{App: testApp.AppVersion()},
		DataHash:       resPrepareProposal.BlockData.Hash,
		ChainID:        chainID,
		Time:           genesisTime.Add(time.Duration(height) * time.Minute),
		Height:         height,
		LastCommitHash: lastCommitHash,
	}

	// Process Proposal
	resProcessProposal := testApp.ProcessProposal(abci.RequestProcessProposal{
		BlockData: resPrepareProposal.BlockData,
		Header:    header,
	},
	)
	if abci.ResponseProcessProposal_ACCEPT != resProcessProposal.Result {
		return nil, nil, fmt.Errorf("ProcessProposal failed: %v", resProcessProposal.Result)
	}

	// Begin block
	validator3Signed := height == 2 // Validator 3 signs only the first block
	testApp.BeginBlock(abci.RequestBeginBlock{
		Header: header,
		LastCommitInfo: abci.LastCommitInfo{
			Votes: []abci.VoteInfo{
				// In order to withdraw commission for this validator
				{
					Validator:       validators[0],
					SignedLastBlock: true,
				},
				// In order to jail this validator
				{
					Validator:       validators[3],
					SignedLastBlock: validator3Signed,
				},
			},
		},
	})

	// Deliver SDK Txs
	for i, tx := range encodedSdkTxs {
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: tx})
		if resp.Code != abci.CodeTypeOK {
			return nil, nil, fmt.Errorf("DeliverTx failed for the message at index %d: %s", i, resp.Log)
		}
	}

	// Deliver Blob Txs
	if len(encodedBlobTx) != 0 {
		// Deliver Blob Tx
		blob, isBlobTx, err := tx.UnmarshalBlobTx(encodedBlobTx)
		if !isBlobTx {
			return nil, nil, fmt.Errorf("Not a valid BlobTx")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("Not a valid BlobTx: %w", err)
		}

		respDeliverTx := testApp.DeliverTx(abci.RequestDeliverTx{Tx: blob.Tx})
		if respDeliverTx.Code != uint32(0) {
			return nil, nil, fmt.Errorf("DeliverTx failed for the BlobTx: %s", respDeliverTx.Log)
		}
	}

	// EndBlock
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})

	// Commit the state
	testApp.Commit()

	// Get the app hash
	appHash := testApp.LastCommitID().Hash

	return dataHash, appHash, nil
}

// createAccounts creates a list of user.Accounts from a list of accountInfos
func createAccounts(accountInfos []blobfactory.AccountInfo, accountNames []string) []*user.Account {
	accounts := make([]*user.Account, 0, len(accountInfos))
	for i, accountInfo := range accountInfos {
		account := user.NewAccount(accountNames[i], accountInfo.AccountNum, accountInfo.Sequence)
		accounts = append(accounts, account)
	}
	return accounts
}

// convertToABCIValidators converts a list of staking.Validator to a list of abci.Validator
func convertToABCIValidators(genValidators []stakingtypes.Validator) ([]abci.Validator, error) {
	abciValidators := make([]abci.Validator, 0, len(genValidators))
	for _, val := range genValidators {
		consAddr, err := val.GetConsAddr()
		if err != nil {
			return nil, err
		}
		abciValidators = append(abciValidators, abci.Validator{
			Address: consAddr,
			Power:   100,
		})
	}
	return abciValidators, nil
}
