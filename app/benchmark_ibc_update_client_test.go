package app_test

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	dbm "github.com/cometbft/cometbft-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	types3 "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	types2 "github.com/cosmos/ibc-go/v6/modules/core/23-commitment/types"
	types4 "github.com/cosmos/ibc-go/v6/modules/light-clients/07-tendermint/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/tmhash"
	crypto2 "github.com/tendermint/tendermint/proto/tendermint/crypto"
	types5 "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/version"
	"math"
	"testing"
	"time"

	"github.com/tendermint/tendermint/crypto/ed25519"
	cmtversion "github.com/tendermint/tendermint/proto/tendermint/version"
	sm "github.com/tendermint/tendermint/state"
	types0 "github.com/tendermint/tendermint/types"
)

func BenchmarkIBC_Update_Client_Multi(b *testing.B) {
	testCases := []struct {
		size int
	}{
		{size: 300},
		//{size: 500},
		//{size: 1000},
		//{size: 5000},
		//{size: 10_000},
		//{size: 50_000},
		//{size: 100_000},
		//{size: 200_000},
		//{size: 300_000},
		//{size: 400_000},
		//{size: 500_000},
		//{size: 1_000_000},
		//{size: 2_000_000},
		//{size: 3_000_000},
		//{size: 4_000_000},
		//{size: 5_000_000},
		//{size: 6_000_000},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("%d bytes", testCase.size), func(b *testing.B) {
			benchmarkIBC_Update_Client(b, testCase.size)
		})
	}
}

func benchmarkIBC_Update_Client(b *testing.B, size int) {
	testApp, rawTx := generateIBCUpdateClientTransaction(b, 1)

	deliverTxRequest := types.RequestDeliverTx{
		Tx: rawTx,
	}

	var resp types.ResponseDeliverTx
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp = testApp.DeliverTx(deliverTxRequest)
	}
	b.StopTimer()
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
	b.ReportMetric(float64(len(rawTx)), "transaction_size(byte)")
}

// generatePayForBlobTransactions creates a test app then generates an IBC
// update client transaction with the specified number of signatures
func generateIBCUpdateClientTransaction(b *testing.B, numberOfSignatures int) (*app.App, []byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)

	msg, msg2 := generateUpdateClientTransaction(b, acc.GetAddress().String())
	rawTx, err := signer.CreateTx([]sdk.Msg{msg2, msg}, user.SetGasLimit(2549760000), user.SetFee(10000))
	require.NoError(b, err)
	return testApp, rawTx
}

func generateUpdateClientTransaction(b *testing.B, signer string) (*types3.MsgUpdateClient, *types3.MsgCreateClient) {
	numberOfValidators := 100
	state, _, privVals := makeState(numberOfValidators, 5)
	wBefore := time.Now()
	time.Sleep(time.Second)
	w := time.Now()
	lastResultHash := crypto.CRandBytes(tmhash.Size)
	lastCommitHash := crypto.CRandBytes(tmhash.Size)
	lastBlockHash := crypto.CRandBytes(tmhash.Size)
	lastBlockID := makeBlockID(lastBlockHash, 1000, []byte("hash"))
	header := types5.Header{
		Version:            cmtversion.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID:            state.ChainID,
		Height:             5,
		Time:               w,
		LastCommitHash:     lastCommitHash,
		DataHash:           crypto.CRandBytes(tmhash.Size),
		ValidatorsHash:     state.Validators.Hash(),
		NextValidatorsHash: state.Validators.Hash(),
		ConsensusHash:      crypto.CRandBytes(tmhash.Size),
		AppHash:            crypto.CRandBytes(tmhash.Size),
		LastResultsHash:    lastResultHash,
		EvidenceHash:       crypto.CRandBytes(tmhash.Size),
		ProposerAddress:    crypto.CRandBytes(crypto.AddressSize),
		LastBlockId:        lastBlockID.ToProto(),
	}
	t := types0.Header{
		Version:            cmtversion.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID:            state.ChainID,
		Height:             5,
		Time:               w,
		LastCommitHash:     header.LastCommitHash,
		DataHash:           header.DataHash,
		ValidatorsHash:     header.ValidatorsHash,
		NextValidatorsHash: header.NextValidatorsHash,
		ConsensusHash:      header.ConsensusHash,
		AppHash:            header.AppHash,
		LastResultsHash:    header.LastResultsHash,
		EvidenceHash:       header.EvidenceHash,
		ProposerAddress:    header.ProposerAddress,
		LastBlockID:        lastBlockID,
	}
	header0Hash := t.Hash()
	fmt.Println(header0Hash.Bytes())
	blockID := makeBlockID(header0Hash, 1000, []byte("partshash"))
	commit, err := makeValidCommit(5, blockID, state.Validators, privVals)
	require.NoError(b, err)
	signatures := make([]types5.CommitSig, numberOfValidators)
	validators := make([]*types5.Validator, numberOfValidators)
	for i := 0; i < numberOfValidators; i++ {
		signatures[i] = types5.CommitSig{
			BlockIdFlag:      types5.BlockIDFlag(commit.Signatures[i].BlockIDFlag),
			ValidatorAddress: commit.Signatures[i].ValidatorAddress,
			Timestamp:        commit.Signatures[i].Timestamp,
			Signature:        commit.Signatures[i].Signature,
		}
		validators[i] = &types5.Validator{
			Address:          state.Validators.Validators[i].Address,
			PubKey:           crypto2.PublicKey{Sum: &crypto2.PublicKey_Ed25519{Ed25519: state.Validators.Validators[i].PubKey.Bytes()}},
			VotingPower:      state.Validators.Validators[i].VotingPower,
			ProposerPriority: state.Validators.Validators[i].ProposerPriority,
		}
	}
	sh := types5.SignedHeader{
		Header: &header,
		Commit: &types5.Commit{
			Height: commit.Height,
			Round:  commit.Round,
			BlockID: types5.BlockID{
				Hash: header0Hash,
				PartSetHeader: types5.PartSetHeader{
					Total: commit.BlockID.PartSetHeader.Total,
					Hash:  commit.BlockID.PartSetHeader.Hash,
				},
			},
			Signatures: signatures,
		},
	}
	clientState := types4.ClientState{
		ChainId:         chainID,
		TrustLevel:      types4.Fraction{Numerator: 1, Denominator: 1}, // we want all signatures to be verified
		TrustingPeriod:  time.Hour * 24 * 21 * 100,                     // we want to always accept the upgrade
		UnbondingPeriod: time.Hour * 24 * 21 * 101,
		MaxClockDrift:   math.MaxInt64 - 1,
		FrozenHeight: types3.Height{
			RevisionNumber: 0,
			RevisionHeight: 0,
		},
		LatestHeight: types3.Height{
			RevisionNumber: 0,
			RevisionHeight: 4,
		},
		ProofSpecs:                   types2.GetSDKSpecs(),
		UpgradePath:                  nil,
		AllowUpdateAfterExpiry:       true,
		AllowUpdateAfterMisbehaviour: true,
	}
	consensusState := types4.ConsensusState{
		Timestamp:          wBefore,
		Root:               types2.MerkleRoot{Hash: lastBlockHash},
		NextValidatorsHash: state.Validators.Hash(),
	}
	createClientMsg, err := types3.NewMsgCreateClient(&clientState, &consensusState, signer)
	require.NoError(b, err)
	msg, err := types3.NewMsgUpdateClient(
		"test_client",
		&types4.Header{
			SignedHeader: &sh,
			ValidatorSet: &types5.ValidatorSet{
				Validators: validators,
				Proposer: &types5.Validator{
					Address:          state.Validators.Proposer.Address,
					PubKey:           crypto2.PublicKey{Sum: &crypto2.PublicKey_Ed25519{Ed25519: state.Validators.Proposer.PubKey.Bytes()}},
					VotingPower:      state.Validators.Proposer.VotingPower,
					ProposerPriority: state.Validators.Proposer.ProposerPriority,
				},
				TotalVotingPower: state.Validators.TotalVotingPower(),
			},
			TrustedHeight: types3.Height{
				RevisionNumber: 0,
				RevisionHeight: 4,
			},
			TrustedValidators: &types5.ValidatorSet{
				Validators: validators,
				Proposer: &types5.Validator{
					Address:          state.Validators.Proposer.Address,
					PubKey:           crypto2.PublicKey{Sum: &crypto2.PublicKey_Ed25519{Ed25519: state.Validators.Proposer.PubKey.Bytes()}},
					VotingPower:      state.Validators.Proposer.VotingPower,
					ProposerPriority: state.Validators.Proposer.ProposerPriority,
				},
				TotalVotingPower: state.Validators.TotalVotingPower(),
			},
		},
		signer,
	)
	require.NoError(b, err)

	return msg, createClientMsg
}

func dummy() {
	//state, stateDB, privVals := makeState(100, 5)
	//stateStore := sm.NewStore(stateDB, sm.StoreOptions{
	//	DiscardABCIResponses: false,
	//})
	//
	//defaultEvidenceTime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	//privVal := privVals[state.Validators.Validators[0].Address.String()]
	//blockID := makeBlockID([]byte("headerhash"), 1000, []byte("partshash"))
	//header := &types0.Header{
	//	Version:            cmtversion.Consensus{Block: version.BlockProtocol, App: 1},
	//	ChainID:            state.ChainID,
	//	Height:             10,
	//	Time:               defaultEvidenceTime,
	//	LastBlockID:        blockID,
	//	LastCommitHash:     crypto.CRandBytes(tmhash.Size),
	//	DataHash:           crypto.CRandBytes(tmhash.Size),
	//	ValidatorsHash:     state.Validators.Hash(),
	//	NextValidatorsHash: state.Validators.Hash(),
	//	ConsensusHash:      crypto.CRandBytes(tmhash.Size),
	//	AppHash:            crypto.CRandBytes(tmhash.Size),
	//	LastResultsHash:    crypto.CRandBytes(tmhash.Size),
	//	EvidenceHash:       crypto.CRandBytes(tmhash.Size),
	//	ProposerAddress:    crypto.CRandBytes(crypto.AddressSize),
	//}

	//// we don't need to worry about validating the evidence as long as they pass validate basic
	//dve := types0.NewMockDuplicateVoteEvidenceWithValidator(3, defaultEvidenceTime, privVal, state.ChainID)
	//dve.ValidatorPower = 1000
	//lcae := &types0.LightClientAttackEvidence{
	//	ConflictingBlock: &types0.LightBlock{
	//		SignedHeader: &types0.SignedHeader{
	//			Header: header,
	//			Commit: types0.NewCommit(10, 0, makeBlockID(header.Hash(), 100, []byte("partshash")), []types0.CommitSig{{
	//				BlockIDFlag:      types0.BlockIDFlagNil,
	//				ValidatorAddress: crypto.AddressHash([]byte("validator_address")),
	//				Timestamp:        defaultEvidenceTime,
	//				Signature:        crypto.CRandBytes(types0.MaxSignatureSize),
	//			}}),
	//		},
	//		ValidatorSet: state.Validators,
	//	},
	//	CommonHeight:        8,
	//	ByzantineValidators: []*types0.Validator{state.Validators.Validators[0]},
	//	TotalVotingPower:    12,
	//	Timestamp:           defaultEvidenceTime,
	//}
}

var chainID string = "test"

func genValSet(size int) (*types0.ValidatorSet, []ed25519.PrivKey) {
	vals := make([]*types0.Validator, size)
	privateKeys := make([]ed25519.PrivKey, size)
	for i := 0; i < size; i++ {
		privateKey := ed25519.GenPrivKey()
		vals[i] = types0.NewValidator(privateKey.PubKey(), 10)
		privateKeys[i] = privateKey
	}
	return types0.NewValidatorSet(vals), privateKeys
}

func makeState(nVals, height int) (sm.State, dbm.DB, map[string]types0.PrivValidator) {
	vals := make([]types0.GenesisValidator, nVals)
	privVals := make(map[string]types0.PrivValidator, nVals)
	for i := 0; i < nVals; i++ {
		secret := []byte(fmt.Sprintf("test%d", i))
		pk := ed25519.GenPrivKeyFromSecret(secret)
		valAddr := pk.PubKey().Address()
		vals[i] = types0.GenesisValidator{
			Address: valAddr,
			PubKey:  pk.PubKey(),
			Power:   1000,
			Name:    fmt.Sprintf("test%d", i),
		}
		privVals[valAddr.String()] = types0.NewMockPVWithParams(pk, false, false)
	}
	s, _ := sm.MakeGenesisState(&types0.GenesisDoc{
		ChainID:    chainID,
		Validators: vals,
		AppHash:    nil,
	})

	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: false,
	})
	if err := stateStore.Save(s); err != nil {
		panic(err)
	}

	for i := 1; i < height; i++ {
		s.LastBlockHeight++
		s.LastValidators = s.Validators.Copy()
		if err := stateStore.Save(s); err != nil {
			panic(err)
		}
	}

	return s, stateDB, privVals
}
func makeValidCommit(
	height int64,
	blockID types0.BlockID,
	vals *types0.ValidatorSet,
	privVals map[string]types0.PrivValidator,
) (*types0.Commit, error) {
	sigs := make([]types0.CommitSig, 0)
	for i := 0; i < vals.Size(); i++ {
		_, val := vals.GetByIndex(int32(i))
		vote, err := types0.MakeVote(height, blockID, vals, privVals[val.Address.String()], chainID, time.Now())
		if err != nil {
			return nil, err
		}
		sigs = append(sigs, vote.CommitSig())
	}
	return types0.NewCommit(height, 0, blockID, sigs), nil
}

func makeBlockID(hash []byte, partSetSize uint32, partSetHash []byte) types0.BlockID {
	var (
		h   = make([]byte, tmhash.Size)
		psH = make([]byte, tmhash.Size)
	)
	copy(h, hash)
	copy(psH, partSetHash)
	return types0.BlockID{
		Hash: h,
		PartSetHeader: types0.PartSetHeader{
			Total: partSetSize,
			Hash:  psH,
		},
	}
}
