package app_test

import (
	"fmt"
	// "os"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	// "github.com/celestiaorg/celestia-app/v2/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	// "github.com/celestiaorg/celestia-app/v2/test/util/testnode"

	// blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	// "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/codec"
	hd "github.com/cosmos/cosmos-sdk/crypto/hd"

	abci "github.com/tendermint/tendermint/abci/types"

	"strconv"

	appns "github.com/celestiaorg/go-square/namespace"
	keyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/stretchr/testify/require"
	// tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestNonDeterminismBetweenAppVersions(t *testing.T) {
	// set up testapp with genesis state
	const (
		numBlobTxs, numNormalTxs = 5, 5
		account                  = "test"
	)
	accounts := deterministicAccounts(numBlobTxs + numNormalTxs)
	
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	// ctx := testApp.NewContext(true, tmproto.Header{})
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts = append(accounts, account)
	kr, _ := DeterministicKeyRing(enc.Codec, accounts...)
	// addr := testfactory.GetAddress(kr, account)
	// acc := testutil.DirectQueryAccount(testApp, addr)
	// signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	// require.NoError(t, err)
	accinfos := queryAccountInfo(testApp, accounts, kr)
	fmt.Println(kr, "kr")


	// create deterministic set of 10 transactions
	normalTxs := testutil.SendTxsWithAccounts(
		t,
		testApp,
		enc.TxConfig,
		kr,
		1000,
		account,
		accounts[:numNormalTxs],
		testutil.ChainID,
	)

	// maybe change this to signer.CreatePFBS
	blobTxs := blobfactory.ManyMultiBlobTx(t, enc.TxConfig, kr, testutil.ChainID, accounts[numBlobTxs:], accinfos[numBlobTxs:], testfactory.Repeat([]*blob.Blob{
		blob.New(HardcodedNamespace(), []byte{1}, appconsts.DefaultShareVersion),
	}, numBlobTxs))
	// normal sdk tx

	// deliver normal txs
	for _, tx := range normalTxs {
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: tx})
		require.EqualValues(t, 0, resp.Code, resp.Log)
	}

	// deliver blob txs
	for _, tx := range blobTxs {
		blobTx, ok := blob.UnmarshalBlobTx(tx)
		require.True(t, ok)
		resp := testApp.DeliverTx(abci.RequestDeliverTx{Tx: blobTx.Tx})
		require.EqualValues(t, 0, resp.Code, resp.Log)
	}

	// Commit the state
	testApp.Commit()

	// // Get the app hash
	appHash := testApp.LastCommitID().Hash

	fmt.Println("AppHash:", appHash)
}

func deterministicAccounts(numAccounts uint64) []string {
	const charset = "abcdefghijklmnopqrs"
	accounts := make([]string, numAccounts)
	for i := range accounts {
		accounts[i] = charset + strconv.Itoa(i)
	}
	return accounts
}

func HardcodedNamespace() appns.Namespace {
	return appns.Namespace{
		Version: 0,
		ID:      []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 37, 67, 154, 200, 228, 130, 74, 147, 162, 11},
	}
}

func DeterministicKeyRing(cdc codec.Codec, accounts ...string) (keyring.Keyring, []sdk.AccAddress) {
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
	addresses := make([]sdk.AccAddress, len(accounts))
	for idx, acc := range accounts {
		rec, err := kb.NewAccount(acc, mnemonics[idx], "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}
		addresses[idx] = addr

	}
	return kb, nil
}
