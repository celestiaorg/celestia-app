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

	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisisTypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"

	// "github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	// slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
)

// PreBlock -> prepare proposal (set of txs) -> PostBlock + Data Root -> process proposal (compare data roots) Data Root is correct (compare to v1)
// begin block -> Post Block -> deliver tx -> end block -> commit (compare state roots)

// for _, block := range blocks {
// 	// prepare
// 	// proces

// 	...
// 	appHash := Commit()
// 	nextHeader.AppHash = appHash
// }

type SdkTx struct {
	sdkMsgs   []sdk.Msg
	txOptions []user.TxOption
}

type BlobTx struct {
	author    string
	blobs     []*blob.Blob
	txOptions []user.TxOption
}

type Tx struct {
	sdkTx  SdkTx
	blobTx BlobTx
}

type Block struct {
	txs []Tx
}

// TestConsistentAppHash executes a set of txs, generates an app hash,
// and compares it against a previously generated hash from the same set of transactions.
// App hashes across different commits should be consistent.
func TestConsistentAppHash(t *testing.T) {
	// Expected app hash produced by v1.x - TODO: link to the test producing the hash
	// expectedAppHash := []byte{9, 208, 117, 101, 108, 61, 146, 58, 26, 190, 199, 124, 76, 178, 84, 74, 54, 159, 76, 187, 2, 169, 128, 87, 70, 78, 8, 192, 28, 144, 116, 117}

	// Initialize testApp
	testApp := testutil.NewTestApp()

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// Create deterministic keys
	kr, pubKeys := deterministicKeyRing(enc.Codec)

	recs, err := kr.List()
	require.NoError(t, err)
	accountNames := make([]string, 0, len(recs))
	accountAddresses := make([]sdk.AccAddress, 0, len(recs))

	// Get the name of the records
	for _, rec := range recs {
		accountNames = append(accountNames, rec.Name)
		accAddress, err := rec.GetAddress()
		require.NoError(t, err)
		accountAddresses = append(accountAddresses, accAddress)
	}

	// Apply genesis state to the app.
	_, _, err = testutil.SetupDeterministicGenesisState(testApp, pubKeys, 20_000_000_000, app.DefaultInitialConsensusParams())
	require.NoError(t, err)

	// Query keyring account infos
	accountInfos := queryAccountInfo(testApp, accountNames, kr)

	// Create accounts for the signer
	accounts := make([]*user.Account, 0, len(accountInfos))
	for i, accountInfo := range accountInfos {
		account := user.NewAccount(accountNames[i], accountInfo.AccountNum, accountInfo.Sequence)
		accounts = append(accounts, account)
	}

	// Create a signer with keyring accounts
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, app.DefaultInitialVersion, accounts...)
	require.NoError(t, err)

	amount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1_000)))

	depositAmount := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(10000000000)))

	validators := testApp.StakingKeeper.GetAllValidators(testApp.NewContext(false, tmproto.Header{}))

	oneInt := sdk.OneInt().Add(sdk.OneInt())

	// ----------- Create SDK Messages ------------

	// ---------------- First Block ------------
	var sdkMessages []sdk.Msg

	// Send funds to another account
	sendFundsMsg := banktypes.NewMsgSend(accountAddresses[0], accountAddresses[1], amount)
	sdkMessages = append(sdkMessages, sendFundsMsg)

	// MultiSend funds to another account
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
	sdkMessages = append(sdkMessages, multiSendFundsMsg)

	// Create a new MsgGrant
	authorization := authz.NewGenericAuthorization(blobtypes.URLMsgPayForBlobs)
	msgGrant, err := authz.NewMsgGrant(
		accountAddresses[0],
		accountAddresses[1],
		authorization,
		&expiration,
	)
	require.NoError(t, err)
	sdkMessages = append(sdkMessages, msgGrant)

	// Create a new MsgVerifyInvariant
	msgVerifyInvariant := crisisTypes.NewMsgVerifyInvariant(accountAddresses[0], banktypes.ModuleName, "nonnegative-outstanding")
	sdkMessages = append(sdkMessages, msgVerifyInvariant)

	// Create a new MsgGrantAllowance
	basicAllowance := feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewIntFromUint64(1000))),
	}
	feegrantMsg, err := feegrant.NewMsgGrantAllowance(&basicAllowance, accountAddresses[0], accountAddresses[1])
	require.NoError(t, err)
	sdkMessages = append(sdkMessages, feegrantMsg)

	// Create a new MsgSubmitProposal
	govAccount := testApp.GovKeeper.GetGovernanceAccount(testApp.NewContext(false, tmproto.Header{})).GetAddress()
	msgSend := banktypes.MsgSend{
		FromAddress: govAccount.String(),
		ToAddress:   accountAddresses[1].String(),
		Amount:      amount,
	}
	proposal, err := govtypes.NewMsgSubmitProposal([]sdk.Msg{&msgSend}, amount, accountAddresses[0].String(), "")
	require.NoError(t, err)
	sdkMessages = append(sdkMessages, proposal)

	msgDeposit := govtypes.NewMsgDeposit(accountAddresses[0], 1, depositAmount)
	sdkMessages = append(sdkMessages, msgDeposit)

	// MsgUnjail requires a validator to be jailed which requires us to manipu
	// valConsAddr, err := validators[0].GetConsAddr()
	// require.NoError(t, err)
	// testApp.StakingKeeper.Jail(testApp.NewContext(false, tmproto.Header{}), valConsAddr)
	// msgUnjail := slashingtypes.NewMsgUnjail(validators[0].GetOperator())
	// sdkMessages = append(sdkMessages, msgUnjail)

	// Create a new MsgCreateValidator
	msgCreateValidator, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(accountAddresses[0]),
		ed25519.GenPrivKeyFromSecret([]byte("validator")).PubKey(),
		amount[0],
		stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
		stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 0o2), sdk.NewDecWithPrec(12, 0o2), sdk.NewDecWithPrec(1, 0o2)),
		sdk.OneInt())
	require.NoError(t, err)
	sdkMessages = append(sdkMessages, msgCreateValidator)

	// Create a new MsgDelegate
	msgDelegate := stakingtypes.NewMsgDelegate(accountAddresses[0], validators[0].GetOperator(), amount[0])
	sdkMessages = append(sdkMessages, msgDelegate)

	// Create a new MsgBeginRedelegate
	msgBeginRedelegate := stakingtypes.NewMsgBeginRedelegate(accountAddresses[0], validators[0].GetOperator(), validators[1].GetOperator(), amount[0])
	sdkMessages = append(sdkMessages, msgBeginRedelegate)

	// ------------ Second Block ------------

	// Create a new MsgVote
	msgVote := govtypes.NewMsgVote(accountAddresses[0], 1, govtypes.VoteOption_VOTE_OPTION_YES, "")
	sdkMessages = append(sdkMessages, msgVote)

	// Create a new MsgRevoke
	msgRevoke := authz.NewMsgRevoke(
		accountAddresses[0],
		accountAddresses[1],
		blobtypes.URLMsgPayForBlobs,
	)

	// Create a new MsgExec to execute the revoke message
	msgExec := authz.NewMsgExec(accountAddresses[0], []sdk.Msg{&msgRevoke})
	sdkMessages = append(sdkMessages, &msgExec)

	// Create a new MsgVoteWeighted
	msgVoteWeighted := govtypes.NewMsgVoteWeighted(
		accountAddresses[0],
		1,
		govtypes.WeightedVoteOptions([]*govtypes.WeightedVoteOption{{Option: govtypes.OptionYes, Weight: "1.0"}}), // Cast the slice to the expected type
		"",
	)
	sdkMessages = append(sdkMessages, msgVoteWeighted)

	// Create a new MsgEditValidator
	msgEditValidator := stakingtypes.NewMsgEditValidator(sdk.ValAddress(accountAddresses[0]), stakingtypes.NewDescription("add", "new", "val", "desc", "."), nil, &oneInt)
	sdkMessages = append(sdkMessages, msgEditValidator)

	// Create a new MsgUndelegate
	msgUndelegate := stakingtypes.NewMsgUndelegate(accountAddresses[0], validators[1].GetOperator(), amount[0])
	sdkMessages = append(sdkMessages, msgUndelegate)

	// Create a new MsgDelegate
	msgDelegate = stakingtypes.NewMsgDelegate(accountAddresses[0], validators[0].GetOperator(), amount[0])
	sdkMessages = append(sdkMessages, msgDelegate)

	// Create a new MsgCancelUnboundingDelegation
	// Messages are split in two blocks, this tx is part of the second block therefore the block height is incremented by 2
	blockHeight := testApp.LastBlockHeight() + 2
	msgCancelUnbondingDelegation := stakingtypes.NewMsgCancelUnbondingDelegation(accountAddresses[0], validators[1].GetOperator(), blockHeight, amount[0])
	sdkMessages = append(sdkMessages, msgCancelUnbondingDelegation)

	// Create a new MsgSetWithdrawAddress
	msgSetWithdrawAddress := distribution.NewMsgSetWithdrawAddress(accountAddresses[0], accountAddresses[1])
	sdkMessages = append(sdkMessages, msgSetWithdrawAddress)

	// Create a new MsgRevokeAllowance
	msgRevokeAllowance := feegrant.NewMsgRevokeAllowance(accountAddresses[0], accountAddresses[1])
	sdkMessages = append(sdkMessages, &msgRevokeAllowance)

	// Create a new MsgFundCommunityPool
	msgFundCommunityPool := distribution.NewMsgFundCommunityPool(amount, accountAddresses[0])
	sdkMessages = append(sdkMessages, msgFundCommunityPool)

	// Create a new MsgWithdrawDelegatorReward
	msgWithdrawDelegatorReward := distribution.NewMsgWithdrawDelegatorReward(accountAddresses[0], validators[0].GetOperator())
	sdkMessages = append(sdkMessages, msgWithdrawDelegatorReward)

	// Create a new MsgWithdrawValidatorCommission
	// valAddressFromBech32, err := sdk.ValAddressFromBech32("celestiavaloper1f6dxw6dgm2dchwmer6jyd6r8c4fkxfx4fv8x8e")
	// require.NoError(t, err)
	msgWithdrawValidatorCommission := distribution.NewMsgWithdrawValidatorCommission(validators[0].GetOperator())
	sdkMessages = append(sdkMessages, msgWithdrawValidatorCommission)

	// ------------ Construct Txs ------------
	var txs []Tx

	// Create transactions from the list of messages
	for _, msg := range sdkMessages {
		tx := createSdkTxWithDefaultOptions([]sdk.Msg{msg})

		txs = append(txs, tx)
	}

	// Create a Blob Tx
	blobTx := BlobTx{
		author:    accountNames[1],
		blobs:     []*blob.Blob{blob.New(fixedNamespace(), []byte{1}, appconsts.DefaultShareVersion)},
		txOptions: blobfactory.DefaultTxOpts(),
	}

	txs = append(txs, Tx{sdkTx: SdkTx{}, blobTx: blobTx})

	rawSdkTxs := make([][]byte, 0, len(txs))
	var rawBlobTx []byte
	// Create SDK Txs
	for _, tx := range txs {
		// check if sdk tx
		if isBlobTxEmpty(tx.blobTx) {
			rawSdkTx, err := signer.CreateTx(tx.sdkTx.sdkMsgs, tx.sdkTx.txOptions...)
			signer.SetSequence(accountNames[0], signer.Account(accountNames[0]).Sequence()+1)
			require.NoError(t, err)
			rawSdkTxs = append(rawSdkTxs, rawSdkTx)
		} else {
			rawBlobTx, _, err = signer.CreatePayForBlobs(tx.blobTx.author, tx.blobTx.blobs, tx.blobTx.txOptions...)
			signer.SetSequence(accountNames[0], signer.Account(accountNames[0]).Sequence()+1)
			require.NoError(t, err)
		}
	}

	_, firstBlockCommitHash := executeTxs(t, testApp, []byte{}, rawSdkTxs[:11], testApp.LastCommitID().Hash)

	// Execute the second block
	_, finalAppHash := executeTxs(t, testApp, rawBlobTx, rawSdkTxs[11:], firstBlockCommitHash)

	fmt.Println(finalAppHash, "DATA HASH AND APP HASH")

	// Require that the app hash is equal to the app hash produced on a different commit
	// require.Equal(t, expectedAppHash, appHash)
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

// createSdkTxWithDefaultOptions creates a Tx with default options with the provided message
func createSdkTxWithDefaultOptions(msgs []sdk.Msg) Tx {
	return Tx{
		sdkTx: SdkTx{
			sdkMsgs:   msgs,
			txOptions: blobfactory.DefaultTxOpts(),
		},
		blobTx: BlobTx{},
	}
}

// Helper function to check if BlobTx is "empty"
func isBlobTxEmpty(tx BlobTx) bool {
	return tx.author == "" && (tx.blobs == nil || len(tx.blobs) == 0) && len(tx.txOptions) == 0
}

func executeTxs(t *testing.T, testApp *app.App, rawBlobTx []byte, rawSdkTxs [][]byte, lastCommitHash []byte) ([]byte, []byte) {
	height := testApp.LastBlockHeight() + 1
	chainId := testApp.GetChainID()

	resPrePareProposal := testApp.PrepareProposal(abci.RequestPrepareProposal{
		BlockData: &tmproto.Data{
			Txs: rawSdkTxs,
		},
		ChainId: chainId,
		Height:  height,
		Time:    time.Now(),
	})

	dataHash := resPrePareProposal.BlockData.Hash

	resProcessProposal := testApp.ProcessProposal(abci.RequestProcessProposal{
		BlockData: resPrePareProposal.BlockData,
		Header: core.Header{
			DataHash: resPrePareProposal.BlockData.Hash,
			ChainID:  chainId,
			Version:  version.Consensus{App: testApp.AppVersion()},
			Height:   height,
		},
	},
	)

	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resProcessProposal.Result, "ProcessProposal failed: ", resProcessProposal.Result)

	// BeginBlock
	header := tmproto.Header{
		Version:        version.Consensus{App: 1},
		ChainID:        chainId,
		Height:         height,
		LastCommitHash: lastCommitHash,
	}

	// Begin block
	testApp.BeginBlock(abci.RequestBeginBlock{Header: header})

	for i, rawSdkTx := range rawSdkTxs {
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: rawSdkTx})
		require.Equal(t, uint32(0), resp.Code, "DeliverTx failed for the message at index %d: %s", i, resp.Log)
	}

	if len(rawBlobTx) != 0 {
		// Deliver Blob Tx
		blob, isBlobTx := blob.UnmarshalBlobTx(rawBlobTx)
		require.True(t, isBlobTx, "Not a valid BlobTx")

		respDeliverTx := testApp.DeliverTx(abci.RequestDeliverTx{Tx: blob.Tx})
		require.Equal(t, uint32(0), respDeliverTx.Code, "DeliverTx failed for the BlobTx: ", respDeliverTx.Log)
	}

	// EndBlock
	testApp.EndBlock(abci.RequestEndBlock{Height: header.Height})

	// Commit the state
	testApp.Commit()

	// Get the app hash
	appHash := testApp.LastCommitID().Hash

	return dataHash, appHash
}
