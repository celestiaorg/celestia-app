package app_test

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/da"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/go-square/shares"
	"github.com/celestiaorg/go-square/square"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibctypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
)

func TestProcessProposal(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer, err := user.NewSigner(kr, enc, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)

	// create 4 single blob blobTxs that are signed with valid account numbers
	// and sequences
	blobTxs := blobfactory.ManyMultiBlobTx(
		t, enc, kr, testutil.ChainID, accounts[:4], infos[:4],
		blobfactory.NestedBlobs(
			t,
			testfactory.RandomBlobNamespaces(tmrand.NewRand(), 4),
			[][]int{{100}, {1000}, {420}, {300}},
		),
	)

	// create 3 MsgSend transactions that are signed with valid account numbers
	// and sequences
	sendTxs := testutil.SendTxsWithAccounts(
		t, testApp, enc, kr, 1000, accounts[0], accounts[len(accounts)-3:], testutil.ChainID,
	)

	// block with all blobs included
	validData := func() *tmproto.Data {
		return &tmproto.Data{
			Txs: blobTxs[:3],
		}
	}

	mixedData := validData()
	mixedData.Txs = append(coretypes.Txs(sendTxs).ToSliceOfBytes(), mixedData.Txs...)

	// // create an invalid block by adding an otherwise valid PFB, but an invalid
	// // signature since there's no account
	// badSigBlobTx := testutil.RandBlobTxsWithManualSequence(
	// 	t, enc, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 1, true,
	// )[0]

	// blobTxWithInvalidNonce := testutil.RandBlobTxsWithManualSequence(
	// 	t, enc, kr, 1000, 1, false, testutil.ChainID, accounts[:1], 1, 3, false,
	// )[0]

	// ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	// invalidNamespace, err := appns.New(appns.NamespaceVersionZero, bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	// // expect an error because the input is invalid: it doesn't contain the namespace version zero prefix.
	// assert.Error(t, err)
	// data := bytes.Repeat([]byte{1}, 13)

	type test struct {
		name           string
		input          *tmproto.Data
		mutator        func(*tmproto.Data)
		appVersion     uint64
		expectedResult abci.ResponseProcessProposal_Result
	}

	tests := []test{
		// {
		// 	name:           "valid untouched data",
		// 	input:          validData(),
		// 	mutator:        func(_ *tmproto.Data) {},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_ACCEPT,
		// },
		// {
		// 	name:  "removed first blob tx",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = d.Txs[1:]
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "added an extra blob tx",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = append(d.Txs, blobTxs[3])
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "modified a blobTx",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0] = &blob.Blob{
		// 			NamespaceId:      ns1.ID,
		// 			Data:             data,
		// 			NamespaceVersion: uint32(ns1.Version),
		// 			ShareVersion:     uint32(appconsts.ShareVersionZero),
		// 		}
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "invalid namespace TailPadding",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0] = &blob.Blob{
		// 			NamespaceId:      appns.TailPaddingNamespace.ID,
		// 			Data:             data,
		// 			NamespaceVersion: uint32(appns.TailPaddingNamespace.Version),
		// 			ShareVersion:     uint32(appconsts.ShareVersionZero),
		// 		}
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "invalid namespace TxNamespace",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0] = &blob.Blob{
		// 			NamespaceId:      appns.TxNamespace.ID,
		// 			Data:             data,
		// 			NamespaceVersion: uint32(appns.TxNamespace.Version),
		// 			ShareVersion:     uint32(appconsts.ShareVersionZero),
		// 		}
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "invalid namespace ParityShares",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0] = &blob.Blob{
		// 			NamespaceId:      appns.ParitySharesNamespace.ID,
		// 			Data:             data,
		// 			NamespaceVersion: uint32(appns.ParitySharesNamespace.Version),
		// 			ShareVersion:     uint32(appconsts.ShareVersionZero),
		// 		}
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "invalid blob namespace",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0] = &blob.Blob{
		// 			NamespaceId:      invalidNamespace.ID,
		// 			Data:             data,
		// 			ShareVersion:     uint32(appconsts.ShareVersionZero),
		// 			NamespaceVersion: uint32(invalidNamespace.Version),
		// 		}
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "pfb namespace version does not match blob",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		blobTx, _ := blob.UnmarshalBlobTx(blobTxs[0])
		// 		blobTx.Blobs[0].NamespaceVersion = appns.NamespaceVersionMax
		// 		blobTxBytes, _ := blob.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		// 		d.Txs[0] = blobTxBytes
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "invalid namespace in index wrapper tx",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		index := 4
		// 		tx, b := blobfactory.IndexWrappedTxWithInvalidNamespace(t, tmrand.NewRand(), signer, uint32(index))
		// 		blobTx, err := blob.MarshalBlobTx(tx, b)
		// 		require.NoError(t, err)

		// 		// Replace the data with new contents
		// 		d.Txs = [][]byte{blobTx}

		// 		// Erasure code the data to update the data root so this doesn't doesn't fail on an incorrect data root.
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "swap blobTxs",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		// swapping the order will cause the data root to be different
		// 		d.Txs[0], d.Txs[1], d.Txs[2] = d.Txs[1], d.Txs[2], d.Txs[0]
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "PFB without blobTx",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		btx, _ := coretypes.UnmarshalBlobTx(blobTxs[3])
		// 		d.Txs = append(d.Txs, btx.Tx)
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "undecodable tx with app version 1",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = append([][]byte{tmrand.Bytes(300)}, d.Txs...)
		// 		// Update the data hash so that the test doesn't fail due to an incorrect data root.
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     v1.Version,
		// 	expectedResult: abci.ResponseProcessProposal_ACCEPT,
		// },
		// {
		// 	name:  "undecodable tx with app version 2",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = append([][]byte{tmrand.Bytes(300)}, d.Txs...)
		// 		// Update the data hash so that the test doesn't fail due to an incorrect data root.
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     v2.Version,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "incorrectly sorted; send tx after pfb",
		// 	input: mixedData,
		// 	mutator: func(d *tmproto.Data) {
		// 		// swap txs at index 2 and 3 (essentially swapping a PFB with a normal tx)
		// 		d.Txs[3], d.Txs[2] = d.Txs[2], d.Txs[3]
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "included pfb with bad signature",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = append(d.Txs, badSigBlobTx)
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name:  "included pfb with incorrect nonce",
		// 	input: validData(),
		// 	mutator: func(d *tmproto.Data) {
		// 		d.Txs = append(d.Txs, blobTxWithInvalidNonce)
		// 		d.Hash = calculateNewDataHash(t, d.Txs)
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		// {
		// 	name: "tampered sequence start",
		// 	input: &tmproto.Data{
		// 		Txs: coretypes.Txs(sendTxs).ToSliceOfBytes(),
		// 	},
		// 	mutator: func(d *tmproto.Data) {
		// 		dataSquare, err := square.Construct(d.Txs, appconsts.DefaultSquareSizeUpperBound, appconsts.DefaultSubtreeRootThreshold)
		// 		require.NoError(t, err)

		// 		b := shares.NewEmptyBuilder().ImportRawShare(dataSquare[1].ToBytes())
		// 		b.FlipSequenceStart()
		// 		updatedShare, err := b.Build()
		// 		require.NoError(t, err)
		// 		dataSquare[1] = *updatedShare

		// 		eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
		// 		require.NoError(t, err)

		// 		dah, err := da.NewDataAvailabilityHeader(eds)
		// 		require.NoError(t, err)
		// 		// replace the hash of the prepare proposal response with the hash of a data
		// 		// square with a tampered sequence start indicator
		// 		d.Hash = dah.Hash()
		// 	},
		// 	appVersion:     appconsts.LatestVersion,
		// 	expectedResult: abci.ResponseProcessProposal_REJECT,
		// },
		{
			name:           "should accept a block with an ICA message that is on allowlist",
			input:          dataIcaAllowed(t, signer, testApp),
			mutator:        func(_ *tmproto.Data) {},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:           "should reject a block with an ICA message that is not on allowlist",
			input:          dataIcaDenied(t, signer, testApp),
			mutator:        func(_ *tmproto.Data) {},
			appVersion:     appconsts.LatestVersion,
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()

			resp := testApp.PrepareProposal(abci.RequestPrepareProposal{
				BlockData: tt.input,
				ChainId:   testutil.ChainID,
				Height:    height,
				Time:      blockTime,
			})
			require.Equal(t, len(tt.input.Txs), len(resp.BlockData.Txs))
			tt.mutator(resp.BlockData)
			res := testApp.ProcessProposal(abci.RequestProcessProposal{
				BlockData: resp.BlockData,
				Header: tmproto.Header{
					Height:   1,
					DataHash: resp.BlockData.Hash,
					ChainID:  testutil.ChainID,
					Version: version.Consensus{
						App: tt.appVersion,
					},
				},
			})
			assert.Equal(t, tt.expectedResult, res.Result, fmt.Sprintf("expected %v, got %v", tt.expectedResult, res.Result))
		})
	}
}

func calculateNewDataHash(t *testing.T, txs [][]byte) []byte {
	dataSquare, err := square.Construct(txs, appconsts.DefaultSquareSizeUpperBound, appconsts.DefaultSubtreeRootThreshold)
	require.NoError(t, err)
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	return dah.Hash()
}

func dataIcaAllowed(t *testing.T, signer *user.Signer, testApp *app.App) *tmproto.Data {
	return &tmproto.Data{
		Txs:        [][]byte{icaTxAllowed(t, signer, testApp)},
		SquareSize: 2,
		Hash:       tmrand.Bytes(32),
	}
}

func dataIcaDenied(t *testing.T, signer *user.Signer, testApp *app.App) *tmproto.Data {
	return &tmproto.Data{
		Txs:        [][]byte{icaTxDenied(t, signer, testApp)},
		SquareSize: 2,
		Hash:       tmrand.Bytes(32),
	}
}

func icaTxAllowed(t *testing.T, signer *user.Signer, testApp *app.App) []byte {
	bankSendMsg := banktypes.NewMsgSend(
		signer.Accounts()[0].Address(),
		signer.Accounts()[0].Address(),
		sdk.NewCoins(sdk.NewCoin("utia", math.NewInt(1))),
	)
	data, err := icatypes.SerializeCosmosTx(testApp.AppCodec(), []proto.Message{bankSendMsg})
	require.NoError(t, err)

	icaPacketData := icatypes.InterchainAccountPacketData{
		Type: icatypes.EXECUTE_TX,
		Data: data,
	}
	packetData := icaPacketData.GetBytes()

	base64ProofCommitment := "Cr8JCrwJCm9jb21taXRtZW50cy9wb3J0cy9pY2Fjb250cm9sbGVyLWNvc21vczFlcHF6dWg2bXlyd3JwNHpyOHpqYW1jeWU0bnZra2c5eGQ4eXdhay9jaGFubmVscy9jaGFubmVsLTQzMTAvc2VxdWVuY2VzLzESIOOEJjaDjCHNeUb5Nscs0jS1mz+M4pSEHnWqdBtWCT6VGg4IARgBIAEqBgAC2uGgFiIsCAESKAIE2uGgFiC2yEQJEJWHquHWhg/shpu6fOhyTtt2Jrf90zLAwr0UCyAiLAgBEigEBtrhoBYglv6DW7Udd8HWnGac8Tqmn2XL7BK/ab9FC8SERVGMq9AgIiwIARIoBg7a4aAWIK1Vn+IslEiRV+rjuwsUEytK3cQLJyOMaic6y/OeLjP1ICIsCAESKAgW2uGgFiAkf3L0kNPOb3iWG94x1Oo3F7tBbhTIyAFrzQi+pt6rTiAiLAgBEigKKNrhoBYgTaZg3a6jUz0ZxoCGVMv5Ms5Gi6NPmJMb9dAa2fn+Q6UgIi4IARIHDGja4aAWIBohIJaGaKlZh0VVe2ssuilbDdCi3a0SiB30NGGpltGQmeA4Ii0IARIpDrYB2uGgFiBgbasOp9FmZSOJD++feygAcJYqoaRUFfkzq7ajJQ3LuCAiLQgBEikQpgLa4aAWIAl0SSkvpQjTDxRVrn1CfBfh87LLuW8xmBWLXpOQjt7NICItCAESKRL2BNrhoBYg4MgElmhPULuGOedxNZoAQp1FFnsbG/3yrTPYl4WZa0QgIi8IARIIFPYI2uGgFiAaISAuXh/nYY9vlfQKv/CgyUrPFzhycY1gk3Jw7bqTwF/rMiItCAESKRb4D9rhoBYg2+Rbd6aRYQmx64VbkpBNZ5tTm6ZFoJxSbXhNG1cv8dAgIi0IARIpGNYX2uGgFiCHjG3nSixO/bAilis8FCYwd/EWN9KK7ord/qD8o4JcqCAiLQgBEikatCva4aAWIP1U5ibnw5lnxJXnEgEF+Sezp3ZOfOd5I46hwrtR2qPWICIvCAESCBy0QdrhoBYgGiEgtdteKHmRA4vpiLYbFG0TsL/+/n6O7gdQNmoAiKlzuDMiLggBEioeppYB2uGgFiDyiezkU1qbVkDwyurADIjsoWY/eeML9hW52bHbOWAi+yAiMAgBEgkgopcC2uGgFiAaISAXPLWiPXl93rKeoXd2AVpYx3w5OHcWe/A0Ge/Q1PmPYiIuCAESKiLg3QPa4aAWIOoqmcYC8BjIhzdpVhEecmVjSEJMhkgBxPHPOYd12zckICIwCAESCSSAoQ/a4aAWIBohIGw9qtHlLwOJan8Z8e28eIjH+m3fjYcsqzm8DfJRuvfjIi4IARIqJvD2GtrhoBYghG3zHblcVrp+v9Axn2sLv42ZvZ45A7yqeAMLQGEl4F0gIi4IARIqKu7kP9rhoBYg/hIwv2icPfvG8P4HYhzsob2W7ycD3ocS5Fhyz+EQkmYgIjAIARIJLITCadrhoBYgGiEgWk1O6HRwUZBZpY7Ejowgw+iT5Ol4mDXfhJ9ApsUuAFQiLwgBEiswqrb0AdrhoBYgkvWkqRpMmiFCA2LgEVw1kn4S+t5RBxM4hFg4roSHYRUgCv4BCvsBCgNpYmMSICQrZ6XLY7zMc/Y/5GtZmyJ2QAMyqHA3HbfvmXO5bzLgGgkIARgBIAEqAQAiJwgBEgEBGiCT0INIVWQHpgAwbQ3xCwA723Ie8pK7ZYZBH2hwo7JHnSInCAESAQEaIDqcSz89iIyuzLTmdLt4A/bxEw1B0KMyqIb9r7G5MZ12IiUIARIhAU+RAwuH9y8+5W5OTRgGq0ef/nGD/9Hrspp1uMHidhq8IiUIARIhAeblY7qeOFbxmgbhdyrefFskF77waQ8cVIv8mtFQm2h/IicIARIBARogmn76WwLMVT8S/TTLe3jQH+cXtPNIv3/7dEILgMgj9Hk="
	proofCommitment, err := base64.StdEncoding.DecodeString(base64ProofCommitment)
	require.NoError(t, err)

	msg := &ibctypes.MsgRecvPacket{
		// Packet is inspired by https://arabica.celenium.io/tx/b567c2e11b1c63706efef1f7448c199b7184e4923587000fe57daf4a07ff3f12?tab=messages
		Packet: ibctypes.Packet{
			Data:               packetData,
			DestinationChannel: "channel-1",
			DestinationPort:    "icahost",
			Sequence:           1,
			SourceChannel:      "channel-4310",
			SourcePort:         "icacontroller-cosmos1epqzuh6myrwrp4zr8zjamcye4nvkkg9xd8ywak",
			TimeoutHeight: ibcclienttypes.Height{
				RevisionHeight: 0,
				RevisionNumber: 0,
			},
			TimeoutTimestamp: 1725050827576431600,
		},
		ProofCommitment: proofCommitment,
		ProofHeight: ibcclienttypes.Height{
			RevisionHeight: 23337070,
			RevisionNumber: 0,
		},
		Signer: signer.Accounts()[0].Address().String(),
	}
	options := blobfactory.DefaultTxOpts()
	tx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)
	return tx
}

func icaTxDenied(t *testing.T, signer *user.Signer, testApp *app.App) []byte {
	bankSendMsg := banktypes.NewMsgMultiSend(
		[]banktypes.Input{},
		[]banktypes.Output{},
	)
	data, err := icatypes.SerializeCosmosTx(testApp.AppCodec(), []proto.Message{bankSendMsg})
	require.NoError(t, err)

	icaPacketData := icatypes.InterchainAccountPacketData{
		Type: icatypes.EXECUTE_TX,
		Data: data,
	}
	packetData := icaPacketData.GetBytes()

	base64ProofCommitment := "Cr8JCrwJCm9jb21taXRtZW50cy9wb3J0cy9pY2Fjb250cm9sbGVyLWNvc21vczFlcHF6dWg2bXlyd3JwNHpyOHpqYW1jeWU0bnZra2c5eGQ4eXdhay9jaGFubmVscy9jaGFubmVsLTQzMTAvc2VxdWVuY2VzLzISIDquiSZLIm8Ju/ixet4lX7EBGeKGy9U/sq/Us0QOUpB2Gg4IARgBIAEqBgACktKiFiIsCAESKAIEktKiFiC2yEQJEJWHquHWhg/shpu6fOhyTtt2Jrf90zLAwr0UCyAiLAgBEigEBpLSohYglv6DW7Udd8HWnGac8Tqmn2XL7BK/ab9FC8SERVGMq9AgIiwIARIoBg6S0qIWIK1Vn+IslEiRV+rjuwsUEytK3cQLJyOMaic6y/OeLjP1ICIsCAESKAgWktKiFiAkf3L0kNPOb3iWG94x1Oo3F7tBbhTIyAFrzQi+pt6rTiAiLAgBEigKKJLSohYgTaZg3a6jUz0ZxoCGVMv5Ms5Gi6NPmJMb9dAa2fn+Q6UgIi4IARIHDGiS0qIWIBohIJaGaKlZh0VVe2ssuilbDdCi3a0SiB30NGGpltGQmeA4Ii0IARIpDrYBktKiFiBgbasOp9FmZSOJD++feygAcJYqoaRUFfkzq7ajJQ3LuCAiLQgBEikQpgKS0qIWIAl0SSkvpQjTDxRVrn1CfBfh87LLuW8xmBWLXpOQjt7NICItCAESKRL2BJLSohYg4MgElmhPULuGOedxNZoAQp1FFnsbG/3yrTPYl4WZa0QgIi8IARIIFPYIktKiFiAaISAuXh/nYY9vlfQKv/CgyUrPFzhycY1gk3Jw7bqTwF/rMiItCAESKRb4D5LSohYg2+Rbd6aRYQmx64VbkpBNZ5tTm6ZFoJxSbXhNG1cv8dAgIi0IARIpGNYXktKiFiCHjG3nSixO/bAilis8FCYwd/EWN9KK7ord/qD8o4JcqCAiLQgBEikatCuS0qIWIP1U5ibnw5lnxJXnEgEF+Sezp3ZOfOd5I46hwrtR2qPWICIvCAESCBy0QZbSohYgGiEgFp3aNcVFf63rT01Z8rxXjLJ/TgZj5nsVvlEnb307yuAiLggBEioeppYBltKiFiDyiezkU1qbVkDwyurADIjsoWY/eeML9hW52bHbOWAi+yAiMAgBEgkgopcCltKiFiAaISAXPLWiPXl93rKeoXd2AVpYx3w5OHcWe/A0Ge/Q1PmPYiIuCAESKiLg3QOW0qIWIOoqmcYC8BjIhzdpVhEecmVjSEJMhkgBxPHPOYd12zckICIwCAESCSSAoQ+W0qIWIBohIDavm40RbyC4Jdf6qhzmmxloDqy2vzmXAh17peUQSkvKIi4IARIqJvD2GpbSohYghG3zHblcVrp+v9Axn2sLv42ZvZ45A7yqeAMLQGEl4F0gIi4IARIqKu7kP5bSohYggW/ik03msR9I1j/rGaIl5XI0GbZEMUPKlo9FpEJf7vsgIjAIARIJLNTCaZbSohYgGiEgPS1m/2g8xhPd7xM6POkKxkgW/Eenqw4Ov4hpuiBp03oiLwgBEisw/Lr0AZbSohYgbltN/Lx5PI5oFF0w4duU8Y9MsAa+G/rizXqPr8MTHsIgCv4BCvsBCgNpYmMSIEGHuHOV3KWUJrjJN8dLFI58lKO2aNuJqqnAIIPH4y96GgkIARgBIAEqAQAiJwgBEgEBGiCT0INIVWQHpgAwbQ3xCwA723Ie8pK7ZYZBH2hwo7JHnSInCAESAQEaILnxdvI8Mc7gpMT4TbmIcR/5Mfn9PVOuztEalE+mVrcRIiUIARIhAQcoXIMT+Uq4vBHeA38ZrKdwc+l2Z8YYdDy/7CaERL4jIiUIARIhAY2VB4BJMHuNRQEyUOsTFHc4O5eQCnssKY9+yeQuriuOIicIARIBARog0sT/RdPuxnUrctvKfMj6vNy1nJ1ZbqXU80ch9ndNmi8="
	proofCommitment, err := base64.StdEncoding.DecodeString(base64ProofCommitment)
	require.NoError(t, err)
	msg := &ibctypes.MsgRecvPacket{
		// Packet is inspired by https://arabica.celenium.io/tx/73a0b90498936483ab1ede4786ce432f3a1ad1163558d6bf5dc1058b8756f489?tab=messages
		Packet: ibctypes.Packet{
			Data:               packetData,
			DestinationChannel: "channel-1",
			DestinationPort:    "icahost",
			Sequence:           1,
			SourceChannel:      "channel-4310",
			SourcePort:         "icacontroller-cosmos1epqzuh6myrwrp4zr8zjamcye4nvkkg9xd8ywak",
			TimeoutHeight: ibcclienttypes.Height{
				RevisionHeight: 0,
				RevisionNumber: 0,
			},
			TimeoutTimestamp: 1725136345563512000,
		},
		ProofCommitment: proofCommitment,
		ProofHeight: ibcclienttypes.Height{
			RevisionHeight: 23352464,
			RevisionNumber: 0,
		},
		Signer: signer.Accounts()[0].Address().String(),
	}
	options := blobfactory.DefaultTxOpts()
	tx, err := signer.CreateTx([]sdk.Msg{msg}, options...)
	require.NoError(t, err)
	return tx
}
