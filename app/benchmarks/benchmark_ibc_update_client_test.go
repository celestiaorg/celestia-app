//go:build benchmarks

package benchmarks_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/tmhash"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtprotoversion "github.com/cometbft/cometbft/proto/tendermint/version"
	sm "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"
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

	checkTxReq := types.RequestCheckTx{
		Type: types.CheckTxType_New,
		Tx:   rawTxs[0],
	}

	b.ResetTimer()
	resp, err := testApp.CheckTx(&checkTxReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.Code)
	require.Equal(b, "", resp.Codespace)
	b.ReportMetric(float64(resp.GasUsed), "gas_used")
	b.ReportMetric(float64(len(rawTxs[0])), "transaction_size(byte)")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

func BenchmarkIBC_FinalizeBlock_Update_Client_Multi(b *testing.B) {
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
			benchmarkIBCFinalizeBlockUpdateClient(b, testCase.numberOfValidators)
		})
	}
}

func benchmarkIBCFinalizeBlockUpdateClient(b *testing.B, numberOfValidators int) {
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, 1, 1)

	finalizeBlockReq := types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(blockTime),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    rawTxs,
	}

	b.ResetTimer()
	resp, err := testApp.FinalizeBlock(&finalizeBlockReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, uint32(0), resp.TxResults[0].Code)
	require.Equal(b, "", resp.TxResults[0].Codespace)
	b.ReportMetric(float64(resp.TxResults[0].GasUsed), "gas_used")
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
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, count, count)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	b.ResetTimer()
	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)
	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "prepare_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResp.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResp.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResp.Txs)), "total_gas_used")
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
	testApp, rawTxs := generateIBCUpdateClientTransaction(b, numberOfValidators, count, count)

	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    rawTxs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	require.NoError(b, err)
	require.GreaterOrEqual(b, len(prepareProposalResp.Txs), 1)

	processProposalReq := types.RequestProcessProposal{
		Txs:          prepareProposalResp.Txs,
		Height:       testApp.LastBlockHeight() + 1,
		DataRootHash: prepareProposalResp.DataRootHash,
		SquareSize:   prepareProposalResp.SquareSize,
	}

	b.ResetTimer()
	resp, err := testApp.ProcessProposal(&processProposalReq)
	require.NoError(b, err)
	b.StopTimer()
	require.Equal(b, types.ResponseProcessProposal_ACCEPT, resp.Status)

	b.ReportMetric(float64(b.Elapsed().Nanoseconds()), "process_proposal_time(ns)")
	b.ReportMetric(float64(len(prepareProposalResp.Txs)), "number_of_transactions")
	b.ReportMetric(float64(len(rawTxs[0])), "transactions_size(byte)")
	b.ReportMetric(calculateBlockSizeInMb(prepareProposalResp.Txs), "block_size(mb)")
	b.ReportMetric(float64(calculateTotalGasUsed(testApp, prepareProposalResp.Txs)), "total_gas_used")
	b.ReportMetric(float64(numberOfValidators), "number_of_validators")
	b.ReportMetric(float64(2*numberOfValidators/3), "number_of_verified_signatures")
}

// generateIBCUpdateClientTransaction creates a test app then generates an IBC
// update client transaction with the specified number of validators.
// Note: the number of the verified signatures is: 2 * numberOfValidators / 3
// the offset is just a hack for transactions to be processed by the needed
// ABCI method.
func generateIBCUpdateClientTransaction(b *testing.B, numberOfValidators, numberOfMessages, offsetAccountSequence int) (*app.App, [][]byte) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
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
		rawTx, _, err := signer.CreateTx([]sdk.Msg{msgs[i]}, user.SetGasLimit(25497600000), user.SetFee(100000))
		require.NoError(b, err)
		rawTxs = append(rawTxs, rawTx)
		accountSequence++
		err = signer.SetSequence(account, accountSequence)
		require.NoError(b, err)
	}

	return testApp, rawTxs
}

func generateUpdateClientTransaction(b *testing.B, app *app.App, signer user.Signer, signerAddr, signerName string, numberOfValidators, numberOfMsgs int) []*clienttypes.MsgUpdateClient {
	state, _, privVals := makeState(numberOfValidators, 5)
	wBefore := time.Now()
	time.Sleep(time.Second)
	w := time.Now()
	chainID := state.ChainID
	lastResultHash := crypto.CRandBytes(tmhash.Size)
	lastCommitHash := crypto.CRandBytes(tmhash.Size)
	lastBlockHash := crypto.CRandBytes(tmhash.Size)
	lastBlockID := makeBlockID(lastBlockHash, 1000, []byte("hash"))
	header := cmtproto.Header{
		Version:            cmtprotoversion.Consensus{Block: version.BlockProtocol, App: 1},
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
	t := cmttypes.Header{
		Version:            cmtprotoversion.Consensus{Block: version.BlockProtocol, App: 1},
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
	signatures := make([]cmtproto.CommitSig, numberOfValidators)
	validators := make([]*cmtproto.Validator, numberOfValidators)
	for i := 0; i < numberOfValidators; i++ {
		signatures[i] = cmtproto.CommitSig{
			BlockIdFlag:      cmtproto.BlockIDFlag(commit.Signatures[i].BlockIDFlag),
			ValidatorAddress: commit.Signatures[i].ValidatorAddress,
			Timestamp:        commit.Signatures[i].Timestamp,
			Signature:        commit.Signatures[i].Signature,
		}
		validators[i] = &cmtproto.Validator{
			Address:          state.Validators.Validators[i].Address,
			PubKey:           cmtprotocrypto.PublicKey{Sum: &cmtprotocrypto.PublicKey_Ed25519{Ed25519: state.Validators.Validators[i].PubKey.Bytes()}},
			VotingPower:      state.Validators.Validators[i].VotingPower,
			ProposerPriority: state.Validators.Validators[i].ProposerPriority,
		}
	}
	sh := cmtproto.SignedHeader{
		Header: &header,
		Commit: &cmtproto.Commit{
			Height: commit.Height,
			Round:  commit.Round,
			BlockID: cmtproto.BlockID{
				Hash: header0Hash,
				PartSetHeader: cmtproto.PartSetHeader{
					Total: commit.BlockID.PartSetHeader.Total,
					Hash:  commit.BlockID.PartSetHeader.Hash,
				},
			},
			Signatures: signatures,
		},
	}
	clientState := ibctm.ClientState{
		ChainId:         chainID,
		TrustLevel:      ibctm.Fraction{Numerator: 1, Denominator: 3},
		TrustingPeriod:  time.Hour * 24 * 21 * 100, // we want to always accept the upgrade
		UnbondingPeriod: time.Hour * 24 * 21 * 101,
		MaxClockDrift:   math.MaxInt64 - 1,
		FrozenHeight:    clienttypes.Height{},
		LatestHeight: clienttypes.Height{
			RevisionNumber: 0,
			RevisionHeight: 4,
		},
		ProofSpecs:                   commitmenttypes.GetSDKSpecs(),
		AllowUpdateAfterExpiry:       true,
		AllowUpdateAfterMisbehaviour: true,
	}
	consensusState := ibctm.ConsensusState{
		Timestamp:          wBefore,
		Root:               commitmenttypes.MerkleRoot{Hash: lastBlockHash},
		NextValidatorsHash: state.Validators.Hash(),
	}

	msgs := make([]*clienttypes.MsgUpdateClient, numberOfMsgs)
	for index := 0; index < numberOfMsgs; index++ {
		createClientMsg, err := clienttypes.NewMsgCreateClient(&clientState, &consensusState, signerAddr)
		require.NoError(b, err)
		rawTx, _, err := signer.CreateTx([]sdk.Msg{createClientMsg}, user.SetGasLimit(2549760000), user.SetFee(10000))
		require.NoError(b, err)
		resp, err := app.FinalizeBlock(&types.RequestFinalizeBlock{
			Height: app.LastBlockHeight() + 1,
			Time:   testutil.GenesisTime.Add(blockTime),
			Hash:   app.LastCommitID().Hash,
			Txs:    [][]byte{rawTx},
		})
		require.NoError(b, err)

		var clientName string
		for _, res := range resp.TxResults {
			for _, event := range res.Events {
				if event.Type == clienttypes.EventTypeCreateClient {
					for _, attribute := range event.Attributes {
						if string(attribute.Key) == clienttypes.AttributeKeyClientID {
							clientName = string(attribute.Value)
						}
					}
				}
			}
			require.NotEmpty(b, clientName)
		}

		msg, err := clienttypes.NewMsgUpdateClient(
			clientName,
			&ibctm.Header{
				SignedHeader: &sh,
				ValidatorSet: &cmtproto.ValidatorSet{
					Validators: validators,
					Proposer: &cmtproto.Validator{
						Address:          state.Validators.Proposer.Address,
						PubKey:           cmtprotocrypto.PublicKey{Sum: &cmtprotocrypto.PublicKey_Ed25519{Ed25519: state.Validators.Proposer.PubKey.Bytes()}},
						VotingPower:      state.Validators.Proposer.VotingPower,
						ProposerPriority: state.Validators.Proposer.ProposerPriority,
					},
					TotalVotingPower: state.Validators.TotalVotingPower(),
				},
				TrustedHeight: clienttypes.Height{
					RevisionNumber: 0,
					RevisionHeight: 4,
				},
				TrustedValidators: &cmtproto.ValidatorSet{
					Validators: validators,
					Proposer: &cmtproto.Validator{
						Address:          state.Validators.Proposer.Address,
						PubKey:           cmtprotocrypto.PublicKey{Sum: &cmtprotocrypto.PublicKey_Ed25519{Ed25519: state.Validators.Proposer.PubKey.Bytes()}},
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

func makeState(nVals, height int) (sm.State, dbm.DB, map[string]cmttypes.PrivValidator) {
	vals := make([]cmttypes.GenesisValidator, nVals)
	privVals := make(map[string]cmttypes.PrivValidator, nVals)
	for i := 0; i < nVals; i++ {
		secret := []byte(fmt.Sprintf("test%d", i))
		pk := ed25519.GenPrivKeyFromSecret(secret)
		valAddr := pk.PubKey().Address()
		vals[i] = cmttypes.GenesisValidator{
			Address: valAddr,
			PubKey:  pk.PubKey(),
			Power:   1000,
			Name:    fmt.Sprintf("test%d", i),
		}
		privVals[valAddr.String()] = cmttypes.NewMockPVWithParams(pk, false, false)
	}
	s, _ := sm.MakeGenesisState(&cmttypes.GenesisDoc{
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
	blockID cmttypes.BlockID,
	vals *cmttypes.ValidatorSet,
	privVals map[string]cmttypes.PrivValidator,
) (*cmttypes.Commit, error) {
	sigs := make([]cmttypes.CommitSig, 0)
	for i := 0; i < vals.Size(); i++ {
		_, val := vals.GetByIndex(int32(i))
		vote, err := cmttypes.MakeVote(privVals[val.Address.String()], appconsts.TestChainID, int32(i), height, 0, cmtproto.PrecommitType, blockID, time.Now())
		if err != nil {
			return nil, err
		}
		sigs = append(sigs, vote.CommitSig())
	}
	return &cmttypes.Commit{
		Height:     height,
		Round:      0,
		BlockID:    blockID,
		Signatures: sigs,
	}, nil
}

func makeBlockID(hash []byte, partSetSize uint32, partSetHash []byte) cmttypes.BlockID {
	var (
		h   = make([]byte, tmhash.Size)
		psH = make([]byte, tmhash.Size)
	)
	copy(h, hash)
	copy(psH, partSetHash)
	return cmttypes.BlockID{
		Hash: h,
		PartSetHeader: cmttypes.PartSetHeader{
			Total: partSetSize,
			Hash:  psH,
		},
	}
}
