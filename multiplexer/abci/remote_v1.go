package abci

import (
	"context"
	"math"

	"google.golang.org/grpc"

	abciv2 "github.com/cometbft/cometbft/abci/types"
	cryptov2 "github.com/cometbft/cometbft/proto/tendermint/crypto"
	typesv2 "github.com/cometbft/cometbft/proto/tendermint/types"
	abciv1 "github.com/tendermint/tendermint/abci/types"
	cryptov1 "github.com/tendermint/tendermint/proto/tendermint/crypto"
	typesv1 "github.com/tendermint/tendermint/proto/tendermint/types"
	versionv1 "github.com/tendermint/tendermint/proto/tendermint/version"
)

type RemoteABCIClientV1 struct {
	abciv1.ABCIApplicationClient

	// retainLastHeight is the height is set in finalize block
	// and returned in commit
	commitRetainLastHeight int64
	// endBlockConsensusVersion is the app version got from the end block abci call
	endBlockConsensusAppVersion uint64
	// chainID is required to pass into the headers.
	chainID string
}

// NewRemoteABCIClientV1 returns a new ABCI Client (using ABCI v1).
// The client behaves like Tendermint for the server side (the application side).
func NewRemoteABCIClientV1(conn *grpc.ClientConn, chainID string) *RemoteABCIClientV1 {
	return &RemoteABCIClientV1{
		ABCIApplicationClient: abciv1.NewABCIApplicationClient(conn),
		chainID:               chainID,
	}
}

// ApplySnapshotChunk implements abciv2.ABCI
func (a *RemoteABCIClientV1) ApplySnapshotChunk(req *abciv2.RequestApplySnapshotChunk) (*abciv2.ResponseApplySnapshotChunk, error) {
	resp, err := a.ABCIApplicationClient.ApplySnapshotChunk(
		context.Background(),
		&abciv1.RequestApplySnapshotChunk{
			Index:  req.Index,
			Chunk:  req.Chunk,
			Sender: req.Sender,
		},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseApplySnapshotChunk{
		Result:        abciv2.ResponseApplySnapshotChunk_Result(resp.Result),
		RefetchChunks: resp.RefetchChunks,
		RejectSenders: resp.RejectSenders,
	}, nil
}

// CheckTx implements abciv2.ABCI
func (a *RemoteABCIClientV1) CheckTx(req *abciv2.RequestCheckTx) (*abciv2.ResponseCheckTx, error) {
	resp, err := a.ABCIApplicationClient.CheckTx(context.Background(), &abciv1.RequestCheckTx{
		Tx:   req.Tx,
		Type: abciv1.CheckTxType(req.Type),
	}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseCheckTx{
		Code:      resp.Code,
		Data:      resp.Data,
		Log:       resp.Log,
		Info:      resp.Info,
		GasWanted: resp.GasWanted,
		GasUsed:   resp.GasUsed,
		Events:    abciEventV1ToV2(resp.Events...),
		Codespace: resp.Codespace,
	}, nil
}

// Commit implements abciv2.ABCI
func (a *RemoteABCIClientV1) Commit() (*abciv2.ResponseCommit, error) {
	return &abciv2.ResponseCommit{
		RetainHeight: a.commitRetainLastHeight,
	}, nil
}

// FinalizeBlock implements abciv2.ABCI
func (a *RemoteABCIClientV1) FinalizeBlock(req *abciv2.RequestFinalizeBlock) (*abciv2.ResponseFinalizeBlock, error) {
	appVersion := a.endBlockConsensusAppVersion
	if appVersion == 0 {
		infoResp, err := a.Info(&abciv2.RequestInfo{})
		if err != nil {
			return nil, err
		}

		appVersion = infoResp.AppVersion
	}

	lastBlockId := typesv1.BlockID{}

	if req.Header.LastBlockId.Hash != nil {
		lastBlockId.Hash = req.Header.LastBlockId.Hash
	}
	if req.Header.LastBlockId.PartSetHeader.Hash != nil {
		lastBlockId.PartSetHeader = typesv1.PartSetHeader{
			Total: req.Header.LastBlockId.PartSetHeader.Total,
			Hash:  req.Header.LastBlockId.PartSetHeader.Hash,
		}
	}

	commitInfo := commitInfoV2ToV1(&req.DecidedLastCommit)

	beginBlockResp, err := a.ABCIApplicationClient.BeginBlock(context.Background(), &abciv1.RequestBeginBlock{
		Hash: req.Hash,
		Header: typesv1.Header{
			ChainID: a.chainID,
			Version: versionv1.Consensus{
				Block: 11,
				App:   appVersion,
			},
			Height:             req.Height,
			Time:               req.Time,
			NextValidatorsHash: req.NextValidatorsHash,
			ProposerAddress:    req.ProposerAddress,
			LastBlockId:        lastBlockId,

			// needed for v3 sync
			DataHash:        req.Header.DataHash,
			LastResultsHash: req.Header.LastResultsHash,
			LastCommitHash:  req.Header.LastCommitHash,
			ValidatorsHash:  req.Header.ValidatorsHash,
			ConsensusHash:   req.Header.ConsensusHash,
			AppHash:         req.Header.AppHash,
			EvidenceHash:    req.Header.EvidenceHash,
		},
		LastCommitInfo:      commitInfo,
		ByzantineValidators: evidenceV2ToV1(req.Misbehavior),
	}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	commitBlockResps := make([]*abciv1.ResponseDeliverTx, 0, len(req.Txs))
	for _, tx := range req.Txs {
		commitBlockResp, err := a.ABCIApplicationClient.DeliverTx(context.Background(), &abciv1.RequestDeliverTx{
			Tx: tx,
		}, grpc.WaitForReady(true))
		if err != nil {
			return nil, err
		}

		commitBlockResps = append(commitBlockResps, commitBlockResp)
	}

	endBlockResp, err := a.ABCIApplicationClient.EndBlock(
		context.Background(),
		&abciv1.RequestEndBlock{
			Height: req.Height,
		},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, err
	}

	// convert events
	var events []abciv2.Event
	events = append(events, abciEventV1ToV2(beginBlockResp.Events...)...)
	for _, commitBlockResp := range commitBlockResps {
		events = append(events, abciEventV1ToV2(commitBlockResp.Events...)...)
	}
	events = append(events, abciEventV1ToV2(endBlockResp.Events...)...)

	// convert tx results
	var txResults []*abciv2.ExecTxResult
	for _, commitBlockResp := range commitBlockResps {
		txResults = append(txResults, &abciv2.ExecTxResult{
			Code:      commitBlockResp.Code,
			Data:      commitBlockResp.Data,
			Log:       commitBlockResp.Log,
			Info:      commitBlockResp.Info,
			GasWanted: commitBlockResp.GasWanted,
			GasUsed:   commitBlockResp.GasUsed,
			Events:    abciEventV1ToV2(commitBlockResp.Events...),
			Codespace: commitBlockResp.Codespace,
		})
	}

	// commit result
	commitResp, err := a.ABCIApplicationClient.Commit(context.Background(), &abciv1.RequestCommit{}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	// set the retain height, used in commit noop
	a.commitRetainLastHeight = commitResp.RetainHeight
	// get the app version from the end block response
	a.endBlockConsensusAppVersion = endBlockResp.GetConsensusParamUpdates().Version.AppVersion

	return &abciv2.ResponseFinalizeBlock{
		Events:                events,
		TxResults:             txResults,
		ValidatorUpdates:      validatorUpdatesV1ToV2(endBlockResp.ValidatorUpdates),
		ConsensusParamUpdates: consensusParamsV1ToV2(endBlockResp.ConsensusParamUpdates),
		AppHash:               commitResp.Data,
	}, nil
}

// Info implements abciv2.ABCI
func (a *RemoteABCIClientV1) Info(req *abciv2.RequestInfo) (*abciv2.ResponseInfo, error) {
	resp, err := a.ABCIApplicationClient.Info(context.Background(), &abciv1.RequestInfo{
		Version:      req.Version,
		BlockVersion: req.BlockVersion,
		P2PVersion:   req.P2PVersion,
	}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseInfo{
		Data:             resp.Data,
		Version:          resp.Version,
		AppVersion:       resp.AppVersion,
		LastBlockHeight:  resp.LastBlockHeight,
		LastBlockAppHash: resp.LastBlockAppHash,
	}, nil
}

// InitChain implements abciv2.ABCI
func (a *RemoteABCIClientV1) InitChain(req *abciv2.RequestInitChain) (*abciv2.ResponseInitChain, error) {
	resp, err := a.ABCIApplicationClient.InitChain(context.Background(), &abciv1.RequestInitChain{
		Time:            req.Time,
		ChainId:         req.ChainId,
		ConsensusParams: consensusParamsV2ToV1(req.ConsensusParams),
		Validators:      validatorUpdatesV2ToV1(req.Validators),
		AppStateBytes:   req.AppStateBytes,
		InitialHeight:   req.InitialHeight,
	}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseInitChain{
		ConsensusParams: consensusParamsV1ToV2(resp.ConsensusParams),
		Validators:      validatorUpdatesV1ToV2(resp.Validators),
		AppHash:         resp.AppHash,
	}, nil
}

// ListSnapshots implements abciv2.ABCI
func (a *RemoteABCIClientV1) ListSnapshots(req *abciv2.RequestListSnapshots) (*abciv2.ResponseListSnapshots, error) {
	resp, err := a.ABCIApplicationClient.ListSnapshots(
		context.Background(),
		&abciv1.RequestListSnapshots{},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, err
	}

	snapshots := make([]*abciv2.Snapshot, 0, len(resp.Snapshots))
	for _, snapshot := range resp.Snapshots {
		snapshots = append(snapshots, &abciv2.Snapshot{
			Height: snapshot.Height,
			Format: snapshot.Format,
			Chunks: snapshot.Chunks,
			Hash:   snapshot.Hash,
		})
	}

	return &abciv2.ResponseListSnapshots{
		Snapshots: snapshots,
	}, nil
}

// LoadSnapshotChunk implements abciv2.ABCI
func (a *RemoteABCIClientV1) LoadSnapshotChunk(req *abciv2.RequestLoadSnapshotChunk) (*abciv2.ResponseLoadSnapshotChunk, error) {
	resp, err := a.ABCIApplicationClient.LoadSnapshotChunk(
		context.Background(),
		&abciv1.RequestLoadSnapshotChunk{
			Height: req.Height,
			Format: req.Chunk,
			Chunk:  req.Chunk,
		},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseLoadSnapshotChunk{
		Chunk: resp.GetChunk(),
	}, nil
}

// OfferSnapshot implements abciv2.ABCI
func (a *RemoteABCIClientV1) OfferSnapshot(req *abciv2.RequestOfferSnapshot) (*abciv2.ResponseOfferSnapshot, error) {
	snapshot := &abciv1.Snapshot{}
	if req.GetSnapshot() != nil {
		snapshot = &abciv1.Snapshot{
			Height:   req.Snapshot.Height,
			Format:   req.Snapshot.Format,
			Chunks:   req.Snapshot.Chunks,
			Hash:     req.Snapshot.Hash,
			Metadata: req.Snapshot.Metadata,
		}
	}

	resp, err := a.ABCIApplicationClient.OfferSnapshot(
		context.Background(),
		&abciv1.RequestOfferSnapshot{
			Snapshot:   snapshot,
			AppHash:    req.AppHash,
			AppVersion: req.AppVersion,
		},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseOfferSnapshot{
		Result: abciv2.ResponseOfferSnapshot_Result(resp.Result),
	}, nil
}

// PrepareProposal implements abciv2.ABCI
func (a *RemoteABCIClientV1) PrepareProposal(req *abciv2.RequestPrepareProposal) (*abciv2.ResponsePrepareProposal, error) {
	resp, err := a.ABCIApplicationClient.PrepareProposal(context.Background(), &abciv1.RequestPrepareProposal{
		ChainId: a.chainID,
		BlockData: &typesv1.Data{
			Txs: req.Txs,
		},
		BlockDataSize: math.MaxInt32, // TODO: hardcoded as not available in v0.38 fork
		Height:        req.Height,
		Time:          req.Time,
	},
		grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponsePrepareProposal{
		Txs:          resp.BlockData.Txs,
		SquareSize:   resp.BlockData.SquareSize,
		DataRootHash: resp.BlockData.Hash,
	}, nil
}

// ProcessProposal implements abciv2.ABCI
func (a *RemoteABCIClientV1) ProcessProposal(req *abciv2.RequestProcessProposal) (*abciv2.ResponseProcessProposal, error) {
	appVersion := a.endBlockConsensusAppVersion
	if appVersion == 0 {
		infoResp, err := a.Info(&abciv2.RequestInfo{})
		if err != nil {
			return nil, err
		}
		appVersion = infoResp.AppVersion
	}

	lastBlockId := typesv1.BlockID{}

	if req.Header.LastBlockId.Hash != nil {
		lastBlockId.Hash = req.Header.LastBlockId.Hash
	}
	if req.Header.LastBlockId.PartSetHeader.Hash != nil {
		lastBlockId.PartSetHeader = typesv1.PartSetHeader{
			Total: req.Header.LastBlockId.PartSetHeader.Total,
			Hash:  req.Header.LastBlockId.PartSetHeader.Hash,
		}
	}

	resp, err := a.ABCIApplicationClient.ProcessProposal(context.Background(), &abciv1.RequestProcessProposal{
		Header: typesv1.Header{
			Version: versionv1.Consensus{
				Block: 11,
				App:   appVersion,
			},
			ChainID:            a.chainID,
			Height:             req.Height,
			Time:               req.Time,
			NextValidatorsHash: req.NextValidatorsHash,
			ProposerAddress:    req.ProposerAddress,
			DataHash:           req.DataRootHash,

			ConsensusHash:   req.Header.ConsensusHash,
			AppHash:         req.Header.AppHash,
			EvidenceHash:    req.Header.EvidenceHash,
			ValidatorsHash:  req.Header.ValidatorsHash,
			LastCommitHash:  req.Header.LastCommitHash,
			LastResultsHash: req.Header.LastResultsHash,
			LastBlockId:     lastBlockId,
		},
		BlockData: &typesv1.Data{
			Txs:        req.Txs,
			SquareSize: req.SquareSize,
			Hash:       req.DataRootHash,
		},
	},
		grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	return &abciv2.ResponseProcessProposal{
		Status: abciv2.ResponseProcessProposal_ProposalStatus(resp.Result),
	}, nil

}

// Query implements abciv2.ABCI
func (a *RemoteABCIClientV1) Query(ctx context.Context, req *abciv2.RequestQuery) (*abciv2.ResponseQuery, error) {
	resp, err := a.ABCIApplicationClient.Query(ctx, &abciv1.RequestQuery{
		Data:   req.Data,
		Path:   req.Path,
		Height: req.Height,
		Prove:  req.Prove,
	}, grpc.WaitForReady(true))
	if err != nil {
		return nil, err
	}

	proofOps := &cryptov2.ProofOps{}
	if resp.ProofOps != nil && len(resp.ProofOps.Ops) > 0 {
		proofOps.Ops = make([]cryptov2.ProofOp, 0, len(resp.ProofOps.Ops))
		for _, proofOp := range resp.ProofOps.Ops {
			proofOps.Ops = append(proofOps.Ops, cryptov2.ProofOp{
				Type: proofOp.Type,
				Key:  proofOp.Key,
				Data: proofOp.Data,
			})
		}
	}

	return &abciv2.ResponseQuery{
		Code:      resp.Code,
		Log:       resp.Log,
		Info:      resp.Info,
		Value:     resp.Value,
		Index:     resp.Index,
		Key:       resp.Key,
		ProofOps:  proofOps,
		Height:    resp.Height,
		Codespace: resp.Codespace,
	}, nil
}

// VerifyVoteExtension implements abciv2.ABCI
func (a *RemoteABCIClientV1) VerifyVoteExtension(req *abciv2.RequestVerifyVoteExtension) (*abciv2.ResponseVerifyVoteExtension, error) {
	return &abciv2.ResponseVerifyVoteExtension{}, nil
}

// ExtendVote implements abciv2.ABCI
func (a *RemoteABCIClientV1) ExtendVote(ctx context.Context, req *abciv2.RequestExtendVote) (*abciv2.ResponseExtendVote, error) {
	return &abciv2.ResponseExtendVote{}, nil
}

// abciEventV1ToV2 converts a slice of abciv1.Event to a slice of abciv2.Event.
func abciEventV1ToV2(events ...abciv1.Event) []abciv2.Event {
	v2Events := make([]abciv2.Event, 0, len(events))

	for _, event := range events {
		attributes := make([]abciv2.EventAttribute, 0, len(event.Attributes))
		for _, attr := range event.Attributes {
			attributes = append(attributes, abciv2.EventAttribute{
				Key:   string(attr.Key),
				Value: string(attr.Value),
			})
		}

		v2Events = append(v2Events, abciv2.Event{
			Type:       event.Type,
			Attributes: attributes,
		})
	}

	return v2Events
}

func validatorUpdatesV1ToV2(validators []abciv1.ValidatorUpdate) []abciv2.ValidatorUpdate {
	v2Updates := make([]abciv2.ValidatorUpdate, len(validators))
	for i, validator := range validators {
		var pubKeyV2 cryptov2.PublicKey
		if pubKey := validator.GetPubKey(); pubKey.GetEd25519() != nil {
			pubKeyV2 = cryptov2.PublicKey{
				Sum: &cryptov2.PublicKey_Ed25519{
					Ed25519: pubKey.GetEd25519(),
				},
			}
		} else if pubKey := validator.GetPubKey(); pubKey.GetSecp256K1() != nil {
			pubKeyV2 = cryptov2.PublicKey{
				Sum: &cryptov2.PublicKey_Secp256K1{
					Secp256K1: pubKey.GetSecp256K1(),
				},
			}
		} else {
			continue
		}

		v2Updates[i] = abciv2.ValidatorUpdate{
			PubKey: pubKeyV2,
			Power:  validator.Power,
		}
	}

	return v2Updates
}

func validatorUpdatesV2ToV1(validators []abciv2.ValidatorUpdate) []abciv1.ValidatorUpdate {
	v1Updates := make([]abciv1.ValidatorUpdate, len(validators))
	for i, validator := range validators {
		var pubKeyV1 cryptov1.PublicKey
		if pubKey := validator.GetPubKey(); pubKey.GetEd25519() != nil {
			pubKeyV1 = cryptov1.PublicKey{
				Sum: &cryptov1.PublicKey_Ed25519{
					Ed25519: pubKey.GetEd25519(),
				},
			}
		} else if pubKey := validator.GetPubKey(); pubKey.GetSecp256K1() != nil {
			pubKeyV1 = cryptov1.PublicKey{
				Sum: &cryptov1.PublicKey_Secp256K1{
					Secp256K1: pubKey.GetSecp256K1(),
				},
			}
		} else {
			continue
		}

		v1Updates[i] = abciv1.ValidatorUpdate{
			PubKey: pubKeyV1,
			Power:  validator.Power,
		}
	}

	return v1Updates
}

func consensusParamsV1ToV2(params *abciv1.ConsensusParams) *typesv2.ConsensusParams {
	consensusParamsV2 := &typesv2.ConsensusParams{}
	if blockParams := params.GetBlock(); blockParams != nil {
		consensusParamsV2.Block = &typesv2.BlockParams{
			MaxBytes: blockParams.MaxBytes,
			MaxGas:   blockParams.MaxGas,
		}
	}

	if evidenceParams := params.GetEvidence(); evidenceParams != nil {
		consensusParamsV2.Evidence = &typesv2.EvidenceParams{
			MaxAgeNumBlocks: evidenceParams.MaxAgeNumBlocks,
			MaxAgeDuration:  evidenceParams.MaxAgeDuration,
			MaxBytes:        evidenceParams.MaxBytes,
		}
	}

	if versionParams := params.GetVersion(); versionParams != nil {
		consensusParamsV2.Version = &typesv2.VersionParams{
			App: versionParams.AppVersion,
		}
	}

	if validatorParams := params.GetValidator(); validatorParams != nil {
		consensusParamsV2.Validator = &typesv2.ValidatorParams{
			PubKeyTypes: validatorParams.PubKeyTypes,
		}
	}

	return consensusParamsV2
}

func consensusParamsV2ToV1(params *typesv2.ConsensusParams) *abciv1.ConsensusParams {
	consensusParamsV1 := &abciv1.ConsensusParams{}
	if blockParams := params.GetBlock(); blockParams != nil {
		consensusParamsV1.Block = &abciv1.BlockParams{
			MaxBytes: blockParams.MaxBytes,
			MaxGas:   blockParams.MaxGas,
		}
	}

	if evidenceParams := params.GetEvidence(); evidenceParams != nil {
		consensusParamsV1.Evidence = &typesv1.EvidenceParams{
			MaxAgeNumBlocks: evidenceParams.MaxAgeNumBlocks,
			MaxAgeDuration:  evidenceParams.MaxAgeDuration,
			MaxBytes:        evidenceParams.MaxBytes,
		}
	}

	if versionParams := params.GetVersion(); versionParams != nil {
		consensusParamsV1.Version = &typesv1.VersionParams{
			AppVersion: versionParams.App,
		}
	}

	if validatorParams := params.GetValidator(); validatorParams != nil {
		consensusParamsV1.Validator = &typesv1.ValidatorParams{
			PubKeyTypes: validatorParams.PubKeyTypes,
		}
	}
	return consensusParamsV1
}

func commitInfoV2ToV1(info *abciv2.CommitInfo) abciv1.LastCommitInfo {
	votes := make([]abciv1.VoteInfo, 0, len(info.Votes))
	for _, vote := range info.Votes {
		votes = append(votes, abciv1.VoteInfo{
			Validator: abciv1.Validator{
				Address: vote.Validator.Address,
				Power:   vote.Validator.Power,
			},
			SignedLastBlock: vote.BlockIdFlag != typesv2.BlockIDFlagAbsent,
		})
	}
	return abciv1.LastCommitInfo{
		Round: info.Round,
		Votes: votes,
	}
}

func evidenceV2ToV1(evidence []abciv2.Misbehavior) []abciv1.Evidence {
	v1Evidence := make([]abciv1.Evidence, len(evidence))
	for i, ev := range evidence {
		v1Evidence[i] = abciv1.Evidence{
			Type:   abciv1.EvidenceType(ev.Type),
			Height: ev.Height,
			Validator: abciv1.Validator{
				Address: ev.Validator.Address,
				Power:   ev.Validator.Power,
			},
			Time:             ev.Time,
			TotalVotingPower: ev.TotalVotingPower,
		}
	}

	return v1Evidence
}
