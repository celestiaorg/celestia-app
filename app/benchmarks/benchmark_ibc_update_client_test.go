//go:build bench_abci_methods

package benchmarks_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/tmhash"
	crypto2 "github.com/cometbft/cometbft/proto/tendermint/crypto"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmprotoversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	types3 "github.com/cosmos/ibc-go/v9/modules/core/02-client/types"
	types2 "github.com/cosmos/ibc-go/v9/modules/core/23-commitment/types"
	types4 "github.com/cosmos/ibc-go/v9/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/ed25519"
	sm "github.com/cometbft/cometbft/state"
	types0 "github.com/cometbft/cometbft/types"
)

func BenchmarkIBC_CheckTx_Update_Client_Multi(b *testing.B) {
	testCases := []struct {
		numberOfValidators int
	}{
		{numberOfValidators: 2},
		{numberOfValidators: 10},
		{numberOfValidators: 25},
		{numberOfValidators: 50},
		{numberOfValidators: 75},
		{numberOfValidators: 100},
		{numberOfValidators: 125},
		{numberOfValidators: 150},
		{numberOfValidators: 175},
		{numberOfValidators: 200},
		{numberOfValidators: 225},
		{numberOfValidators: 250},
		{numberOfValidators: 300},
		{numberOfValidators: 400},
		{numberOfValidators: 500},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("number of validators: %d", testCase.numberOfValidators), func(b *testing.B) {
			benchmarkIBCCheckTxUpdateClient(b, testCase.numberOfValidators)
		})
	}
}

func benchmarkIBCCheckTxUpdateClient(b *testing.B, numberOfValidators int) {
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, 1, 1)
	testApp.Commit()

	checkTxRequest := types.RequestCheckTx{
		Type: types.CheckTxType_New,
		Tx:   rawTxs[0],
	}

	b.ResetTimer()
	resp := testApp.CheckTx(checkTxRequest)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
	b.ReportMetric(float64(len(rawTxs[0])), "transaction_size(byte)")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

func BenchmarkIBC_DeliverTx_Update_Client_Multi(b *testing.B) {
	testCases := []struct {
		numberOfValidators int
	}{
		{numberOfValidators: 2},
		{numberOfValidators: 10},
		{numberOfValidators: 25},
		{numberOfValidators: 50},
		{numberOfValidators: 75},
		{numberOfValidators: 100},
		{numberOfValidators: 125},
		{numberOfValidators: 150},
		{numberOfValidators: 175},
		{numberOfValidators: 200},
		{numberOfValidators: 225},
		{numberOfValidators: 250},
		{numberOfValidators: 300},
		{numberOfValidators: 400},
		{numberOfValidators: 500},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("number of validators: %d", testCase.numberOfValidators), func(b *testing.B) {
			benchmarkIBCDeliverTxUpdateClient(b, testCase.numberOfValidators)
		})
	}
}

func benchmarkIBCDeliverTxUpdateClient(b *testing.B, numberOfValidators int) {
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, 1, 1)

	deliverTxRequest := types.RequestDeliverTx{
		Tx: rawTxs[0],
	}

	b.ResetTimer()
	resp := testApp.DeliverTx(deliverTxRequest)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
	b.ReportMetric(float64(len(rawTxs[0])), "transaction_size(byte)")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

func BenchmarkIBC_PrepareProposal_Update_Client_Multi(b *testing.B) {
	testCases := []struct {
		numberOfTransactions, numberOfValidators int
	}{
		{numberOfTransactions: 6_000, numberOfValidators: 2},
		{numberOfTransactions: 3_000, numberOfValidators: 10},
		{numberOfTransactions: 2_000, numberOfValidators: 25},
		{numberOfTransactions: 1_000, numberOfValidators: 50},
		{numberOfTransactions: 500, numberOfValidators: 75},
		{numberOfTransactions: 500, numberOfValidators: 100},
		{numberOfTransactions: 500, numberOfValidators: 125},
		{numberOfTransactions: 500, numberOfValidators: 150},
		{numberOfTransactions: 500, numberOfValidators: 175},
		{numberOfTransactions: 500, numberOfValidators: 200},
		{numberOfTransactions: 500, numberOfValidators: 225},
		{numberOfTransactions: 500, numberOfValidators: 250},
		{numberOfTransactions: 500, numberOfValidators: 300},
		{numberOfTransactions: 500, numberOfValidators: 400},
		{numberOfTransactions: 500, numberOfValidators: 500},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("number of validators: %d", testCase.numberOfValidators), func(b *testing.B) {
			benchmarkIBCPrepareProposalUpdateClient(b, testCase.numberOfValidators, testCase.numberOfTransactions)
		})
	}
}

func benchmarkIBCPrepareProposalUpdateClient(b *testing.B, numberOfValidators, count int) {
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, count, 0)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}

	b.ResetTimer()
	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)
	b.StopTimer()
	require.GreaterOrEqual(b, len(prepareProposalResponse.BlockData.Txs), 1)
	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "prepare_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResponse.BlockData.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResponse.BlockData.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResponse.BlockData.Txs)), "total_gas_used")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

func BenchmarkIBC_ProcessProposal_Update_Client_Multi(b *testing.B) {
	testCases := []struct {
		numberOfTransactions, numberOfValidators int
	}{
		{numberOfTransactions: 6_000, numberOfValidators: 2},
		{numberOfTransactions: 3_000, numberOfValidators: 10},
		{numberOfTransactions: 2_000, numberOfValidators: 25},
		{numberOfTransactions: 1_000, numberOfValidators: 50},
		{numberOfTransactions: 500, numberOfValidators: 75},
		{numberOfTransactions: 500, numberOfValidators: 100},
		{numberOfTransactions: 500, numberOfValidators: 125},
		{numberOfTransactions: 500, numberOfValidators: 150},
		{numberOfTransactions: 500, numberOfValidators: 175},
		{numberOfTransactions: 500, numberOfValidators: 200},
		{numberOfTransactions: 500, numberOfValidators: 225},
		{numberOfTransactions: 500, numberOfValidators: 250},
		{numberOfTransactions: 500, numberOfValidators: 300},
		{numberOfTransactions: 500, numberOfValidators: 400},
		{numberOfTransactions: 500, numberOfValidators: 500},
	}
	for _, testCase := range testCases {
		b.Run(fmt.Sprintf("number of validators: %d", testCase.numberOfValidators), func(b *testing.B) {
			benchmarkIBCProcessProposalUpdateClient(b, testCase.numberOfValidators, testCase.numberOfTransactions)
		})
	}
}

func benchmarkIBCProcessProposalUpdateClient(b *testing.B, numberOfValidators, count int) {
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, count, 0)

	blockData := &tmproto.Data{
		Txs: rawTxs,
	}
	prepareProposalRequest := types.RequestPrepareProposal{
		BlockData: blockData,
		ChainId:   testApp.GetChainID(),
		Height:    10,
	}

	prepareProposalResponse := testApp.PrepareProposal(prepareProposalRequest)
	require.GreaterOrEqual(b, len(prepareProposalResponse.BlockData.Txs), 1)

	processProposalRequest := types.RequestProcessProposal{
		BlockData: prepareProposalResponse.BlockData,
		Header: tmproto.Header{
			Height:   10,
			DataHash: prepareProposalResponse.BlockData.Hash,
			ChainID:  testutil.ChainID,
			Version: tmprotoversion.Consensus{
				App: testApp.AppVersion(),
			},
		},
	}

	b.ResetTimer()
	resp := testApp.ProcessProposal(processProposalRequest)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Result)

	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "process_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResponse.BlockData.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResponse.BlockData.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResponse.BlockData.Txs)), "total_gas_used")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

// generateIBCUpdateClientTransaction creates a test app then generates an IBC
// update client transaction with the specified number of validators.
// Note: the number of the verified signatures is: 2 * numberOfValidators / 3
// the offset is just a hack for transactions to be processed by the needed
// ABCI method.
func generateIBCUpdateClientTransaction(b *testing.B, numberOfValidators int, numberOfMessages int, offsetAccountSequence int) (*app.App, [][]byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)

	msgs := generateUpdateClientTransaction(
		b,
		testApp,
		*signer,
		acc.GetAddress().String(),
		account,
		numberOfValidators,
		numberOfMessages,
	)

	accountSequence := testutil.DirectQueryAccount(testApp, addr).GetSequence()
	err = signer.SetSequence(account, accountSequence+uint64(offsetAccountSequence))
	require.NoError(b, err)
	rawTxs := make([][]byte, 0, numberOfMessages)
	for i := 0; i < numberOfMessages; i++ {
		rawTx, err := signer.CreateTx([]sdk.Msg{msgs[i]}, user.SetGasLimit(25497600000), user.SetFee(100000))
		require.NoError(b, err)
		rawTxs = append(rawTxs, rawTx)
		accountSequence++
		err = signer.SetSequence(account, accountSequence)
		require.NoError(b, err)
	}

	return testApp, rawTxs
}

func generateUpdateClientTransaction(b *testing.B, app *app.App, signer user.Signer, signerAddr string, signerName string, numberOfValidators int, numberOfMsgs int) []*types3.MsgUpdateClient {
	state, _, privVals := makeState(numberOfValidators, 5)
	wBefore := time.Now()
	time.Sleep(time.Second)
	w := time.Now()
	lastResultHash := crypto.CRandBytes(tmhash.Size)
	lastCommitHash := crypto.CRandBytes(tmhash.Size)
	lastBlockHash := crypto.CRandBytes(tmhash.Size)
	lastBlockID := makeBlockID(lastBlockHash, 1000, []byte("hash"))
	header := tmproto.Header{
		Version:            tmprotoversion.Consensus{Block: version.BlockProtocol, App: 1},
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
		Version:            tmprotoversion.Consensus{Block: version.BlockProtocol, App: 1},
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
	blockID := makeBlockID(header0Hash, 1000, []byte("partshash"))
	commit, err := makeValidCommit(5, blockID, state.Validators, privVals)
	require.NoError(b, err)
	signatures := make([]tmproto.CommitSig, numberOfValidators)
	validators := make([]*tmproto.Validator, numberOfValidators)
	for i := 0; i < numberOfValidators; i++ {
		signatures[i] = tmproto.CommitSig{
			BlockIdFlag:      tmproto.BlockIDFlag(commit.Signatures[i].BlockIDFlag),
			ValidatorAddress: commit.Signatures[i].ValidatorAddress,
			Timestamp:        commit.Signatures[i].Timestamp,
			Signature:        commit.Signatures[i].Signature,
		}
		validators[i] = &tmproto.Validator{
			Address:          state.Validators.Validators[i].Address,
			PubKey:           crypto2.PublicKey{Sum: &crypto2.PublicKey_Ed25519{Ed25519: state.Validators.Validators[i].PubKey.Bytes()}},
			VotingPower:      state.Validators.Validators[i].VotingPower,
			ProposerPriority: state.Validators.Validators[i].ProposerPriority,
		}
	}
	sh := tmproto.SignedHeader{
		Header: &header,
		Commit: &tmproto.Commit{
			Height: commit.Height,
			Round:  commit.Round,
			BlockID: tmproto.BlockID{
				Hash: header0Hash,
				PartSetHeader: tmproto.PartSetHeader{
					Total: commit.BlockID.PartSetHeader.Total,
					Hash:  commit.BlockID.PartSetHeader.Hash,
				},
			},
			Signatures: signatures,
		},
	}
	clientState := types4.ClientState{
		ChainId:         chainID,
		TrustLevel:      types4.Fraction{Numerator: 1, Denominator: 3},
		TrustingPeriod:  time.Hour * 24 * 21 * 100, // we want to always accept the upgrade
		UnbondingPeriod: time.Hour * 24 * 21 * 101,
		MaxClockDrift:   math.MaxInt64 - 1,
		FrozenHeight:    types3.Height{},
		LatestHeight: types3.Height{
			RevisionNumber: 0,
			RevisionHeight: 4,
		},
		ProofSpecs:                   types2.GetSDKSpecs(),
		AllowUpdateAfterExpiry:       true,
		AllowUpdateAfterMisbehaviour: true,
	}
	consensusState := types4.ConsensusState{
		Timestamp:          wBefore,
		Root:               types2.MerkleRoot{Hash: lastBlockHash},
		NextValidatorsHash: state.Validators.Hash(),
	}

	msgs := make([]*types3.MsgUpdateClient, numberOfMsgs)
	for index := 0; index < numberOfMsgs; index++ {
		createClientMsg, err := types3.NewMsgCreateClient(&clientState, &consensusState, signerAddr)
		require.NoError(b, err)
		rawTx, err := signer.CreateTx([]sdk.Msg{createClientMsg}, user.SetGasLimit(2549760000), user.SetFee(10000))
		require.NoError(b, err)
		resp := app.DeliverTx(types.RequestDeliverTx{Tx: rawTx})
		var clientName string
		for _, event := range resp.Events {
			if event.Type == types3.EventTypeCreateClient {
				for _, attribute := range event.Attributes {
					if string(attribute.Key) == types3.AttributeKeyClientID {
						clientName = string(attribute.Value)
					}
				}
			}
		}
		require.NotEmpty(b, clientName)

		msg, err := types3.NewMsgUpdateClient(
			clientName,
			&types4.Header{
				SignedHeader: &sh,
				ValidatorSet: &tmproto.ValidatorSet{
					Validators: validators,
					Proposer: &tmproto.Validator{
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
				TrustedValidators: &tmproto.ValidatorSet{
					Validators: validators,
					Proposer: &tmproto.Validator{
						Address:          state.Validators.Proposer.Address,
						PubKey:           crypto2.PublicKey{Sum: &crypto2.PublicKey_Ed25519{Ed25519: state.Validators.Proposer.PubKey.Bytes()}},
						VotingPower:      state.Validators.Proposer.VotingPower,
						ProposerPriority: state.Validators.Proposer.ProposerPriority,
					},
					TotalVotingPower: state.Validators.TotalVotingPower(),
				},
			},
			signerAddr,
		)
		require.NoError(b, err)
		msgs[index] = msg
		err = signer.IncrementSequence(signerName)
		require.NoError(b, err)
	}

	return msgs
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
		ChainID:    appconsts.TestChainID,
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
