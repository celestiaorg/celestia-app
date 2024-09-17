package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	signal "github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisisTypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
)

type BlobTx struct {
	author    string
	blobs     []*blob.Blob
	txOptions []user.TxOption
}

type appHashTest struct {
	name               string
	version            uint64
	encodedSdkMessages func(*testing.T, []sdk.AccAddress, []stakingtypes.Validator, *app.App, *user.Signer, *user.Signer) ([][]byte, [][]byte, [][]byte)
	encodedBlobTxs     func(*user.Signer, []sdk.AccAddress) []byte
	expectedDataRoot   []byte
	expectedAppHash    []byte
}

// TestConsistentAppHash executes all state machine messages on all app versions, generates an app hash,
// and compares it against a previously generated hash from the same set of transactions.
// App hashes across different commits should be consistent.
func TestConsistentAppHash(t *testing.T) {
	tc := []appHashTest{
		{
			name:    "execute sdk messages and blob txs on v1 and assert consistent app hash",
			version: 1,
			encodedSdkMessages: func(t *testing.T, accountAddresses []sdk.AccAddress, genValidators []stakingtypes.Validator, testApp *app.App, signer *user.Signer, valSigner *user.Signer) ([][]byte, [][]byte, [][]byte) {
				return encodedSdkMessagesV1(t, accountAddresses, genValidators, testApp, signer, valSigner)
			},
			encodedBlobTxs: func(signer *user.Signer, accountAddresses []sdk.AccAddress) []byte {
				senderAcc := signer.AccountByAddress(accountAddresses[1])
				newBlob := blob.New(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion)

				// Create a Blob Tx
				blobTx := BlobTx{
					author:    senderAcc.Name(),
					blobs:     []*blob.Blob{newBlob},
					txOptions: blobfactory.DefaultTxOpts(),
				}
				encodedBlobTx, _, err := signer.CreatePayForBlobs(blobTx.author, blobTx.blobs, blobTx.txOptions...)
				require.NoError(t, err)
				return encodedBlobTx
			},
			expectedDataRoot: []byte{100, 59, 112, 241, 238, 49, 50, 64, 105, 90, 209, 211, 49, 254, 211, 83, 133, 88, 5, 89, 221, 116, 141, 72, 33, 110, 16, 78, 5, 48, 118, 72},
			// Expected app hash produced by v1.x -
			expectedAppHash: []byte{84, 216, 210, 48, 113, 204, 234, 21, 150, 236, 97, 87, 242, 184, 45, 248, 116, 127, 49, 88, 134, 197, 202, 125, 44, 210, 67, 144, 107, 51, 145, 65},
		},
		{
			name:    "execute sdk messages and blob txs on v2 and assert consistent app hash",
			version: 2,
			encodedSdkMessages: func(t *testing.T, accountAddresses []sdk.AccAddress, genValidators []stakingtypes.Validator, testApp *app.App, signer *user.Signer, valSigner *user.Signer) ([][]byte, [][]byte, [][]byte) {
				firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs := encodedSdkMessagesV1(t, accountAddresses, genValidators, testApp, signer, valSigner)
				encodedMessagesV2 := encodedSdkMessagesV2(t, genValidators, valSigner)
				thirdBlockEncodedTxs = append(thirdBlockEncodedTxs, encodedMessagesV2...)

				return firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs
			},
			encodedBlobTxs: func(signer *user.Signer, accountAddresses []sdk.AccAddress) []byte {
				senderAcc := signer.AccountByAddress(accountAddresses[1])
				newBlob := blob.New(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion)

				// Create a Blob Tx
				blobTx := BlobTx{
					author:    senderAcc.Name(),
					blobs:     []*blob.Blob{newBlob},
					txOptions: blobfactory.DefaultTxOpts(),
				}
				encodedBlobTx, _, err := signer.CreatePayForBlobs(blobTx.author, blobTx.blobs, blobTx.txOptions...)
				require.NoError(t, err)
				return encodedBlobTx
			},
			expectedDataRoot: []byte{200, 61, 245, 166, 119, 211, 170, 2, 73, 239, 253, 97, 243, 112, 116, 196, 70, 41, 201, 172, 123, 28, 15, 182, 52, 222, 122, 243, 95, 97, 66, 233},
			// Expected app hash produced by an older commit on v2.x
			expectedAppHash: []byte{16, 144, 102, 79, 23, 207, 200, 139, 77, 245, 250, 101, 217, 227, 255, 245, 172, 1, 44, 70, 188, 140, 215, 103, 178, 4, 80, 179, 11, 150, 31, 134},
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
			signer, accountAddresses := getAccountsAndCreateSigner(t, kr, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, testApp)
			// Validators from genesis state
			genValidators := testApp.StakingKeeper.GetAllValidators(testApp.NewContext(false, tmproto.Header{}))
			valSigner, _ := getAccountsAndCreateSigner(t, valKeyRing, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, testApp)

			// Convert validators to ABCI validators
			abciValidators, err := convertToABCIValidators(genValidators)
			require.NoError(t, err)

			firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs := tt.encodedSdkMessages(t, accountAddresses, genValidators, testApp, signer, valSigner)
			encodedBlobTx := tt.encodedBlobTxs(signer, accountAddresses)

			// Execute the first block
			_, firstBlockAppHash, err := executeTxs(testApp, []byte{}, firstBlockEncodedTxs, abciValidators, testApp.LastCommitID().Hash, tt.version)
			require.NoError(t, err)
			// Execute the second block
			_, secondBlockAppHash, err := executeTxs(testApp, encodedBlobTx, secondBlockEncodedTxs, abciValidators, firstBlockAppHash, tt.version)
			require.NoError(t, err)
			// Execute the final block and get the data root alongside the final app hash
			finalDataRoot, finalAppHash, err := executeTxs(testApp, []byte{}, thirdBlockEncodedTxs, abciValidators, secondBlockAppHash, tt.version)
			require.NoError(t, err)
			fmt.Println(finalDataRoot, finalAppHash, tt.version)

			fmt.Println(finalDataRoot, "final data root")
			fmt.Println(finalAppHash, "final app hash")

			// Require that the app hash is equal to the app hash produced on a different commit
			require.Equal(t, tt.expectedAppHash, finalAppHash)
			// Require that the data root is equal to the data root produced on a different commit
			require.Equal(t, tt.expectedDataRoot, finalDataRoot)
		})
	}
}

// getAccountsAndCreateSigner returns a signer with accounts
func getAccountsAndCreateSigner(t *testing.T, kr keyring.Keyring, enc client.TxConfig, chainID string, initialVersion uint64, testApp *app.App) (*user.Signer, []sdk.AccAddress) {
	// Get account names and addresses from the keyring
	accountNames := testfactory.GetAccountNames(kr)
	accountAddresses := testfactory.GetAddresses(kr)
	// Query keyring account infos
	accountInfos := queryAccountInfo(testApp, accountNames, kr)
	// Create accounts for the signer
	accounts := createAccounts(accountInfos, accountNames)
	// Create a signer with accounts
	signer, err := user.NewSigner(kr, enc, chainID, initialVersion, accounts...)
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

	// ------------ Third Block ------------

	// Txs within the third block are signed by the validator's signer
	var thirdBlockSdkMsgs []sdk.Msg

	// NewMsgWithdrawValidatorCommission - withdraws validator-0's commission
	msgWithdrawValidatorCommission := distribution.NewMsgWithdrawValidatorCommission(genValidators[0].GetOperator())
	thirdBlockSdkMsgs = append(thirdBlockSdkMsgs, msgWithdrawValidatorCommission)

	// NewMsgUnjail - unjails validator-3
	msgUnjail := slashingtypes.NewMsgUnjail(genValidators[3].GetOperator())
	thirdBlockSdkMsgs = append(thirdBlockSdkMsgs, msgUnjail)

	firstBlockEncodedTxs, err := processSdkMessages(signer, firstBlockSdkMsgs)
	require.NoError(t, err)
	secondBlockEncodedTxs, err := processSdkMessages(signer, secondBlockSdkMsgs)
	require.NoError(t, err)
	thirdBlockEncodedTxs, err := processSdkMessages(valSigner, thirdBlockSdkMsgs)
	require.NoError(t, err)

	return firstBlockEncodedTxs, secondBlockEncodedTxs, thirdBlockEncodedTxs
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

// fixedNamespace returns a hardcoded namespace
func fixedNamespace() appns.Namespace {
	return appns.Namespace{
		Version: 0,
		ID:      []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 67, 154, 200, 228, 130, 74, 147, 162, 11},
	}
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
func executeTxs(testApp *app.App, encodedBlobTx []byte, encodedSdkTxs [][]byte, validators []abci.Validator, lastCommitHash []byte, appVersion uint64) ([]byte, []byte, error) {
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
		Version:        version.Consensus{App: appVersion},
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
		blob, isBlobTx := blob.UnmarshalBlobTx(encodedBlobTx)
		if !isBlobTx {
			return nil, nil, fmt.Errorf("Not a valid BlobTx")
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
