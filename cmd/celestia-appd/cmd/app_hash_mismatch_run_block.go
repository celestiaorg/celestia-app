package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/app"
	cmtdbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	flagRunBlockMode   = "mode"
	flagRunBlockCommit = "commit"

	runBlockModeFinalizeOnly         = "finalize"
	runBlockModeProcessFinalize      = "process-finalize"
	runBlockModeCheckProcessFinalize = "check-process-finalize"
)

func appHashMismatchRunBlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run-block",
		Short: "Run a historical block through the in-process ABCI execution path",
		Long: `Run a historical block from blockstore against the application state in --home.

The intended workflow is to copy a node home at height H-1, then run block H
through CheckTx/ProcessProposal/FinalizeBlock in-process. By default the command
does not call Commit, so it computes the FinalizeBlock AppHash without writing
the application DB. Pass --commit only when --home points at a disposable copy.

Modes:
  finalize                FinalizeBlock only
  process-finalize        ProcessProposal, then FinalizeBlock
  check-process-finalize  CheckTx(New) for each tx, ProcessProposal, FinalizeBlock`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			height, err := cmd.Flags().GetInt64(flagHeight)
			if err != nil {
				return err
			}
			mode, err := cmd.Flags().GetString(flagRunBlockMode)
			if err != nil {
				return err
			}
			commit, err := cmd.Flags().GetBool(flagRunBlockCommit)
			if err != nil {
				return err
			}

			r := &blockRunner{
				serverCtx: serverCtx,
				home:      serverCtx.Config.RootDir,
				height:    height,
				mode:      mode,
				commit:    commit,
			}
			result, err := r.run()
			if err != nil {
				return err
			}
			bz, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(bz))
			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, app.NodeHome, "node home directory; use a disposable copy when passing --commit")
	cmd.Flags().Int64(flagHeight, 0, "block height H to execute; --home must contain app state at H-1")
	cmd.Flags().String(flagRunBlockMode, runBlockModeCheckProcessFinalize, "execution mode: finalize, process-finalize, check-process-finalize")
	cmd.Flags().Bool(flagRunBlockCommit, false, "call Commit after FinalizeBlock and persist state to --home")
	return cmd
}

type blockRunner struct {
	serverCtx *server.Context
	home      string
	height    int64
	mode      string
	commit    bool
}

type runBlockResult struct {
	Home                     string            `json:"home"`
	Mode                     string            `json:"mode"`
	Committed                bool              `json:"committed"`
	Height                   int64             `json:"height"`
	CometLastBlockHeight     int64             `json:"comet_last_block_height,omitempty"`
	AppStartHeight           int64             `json:"app_start_height"`
	AppStartHash             string            `json:"app_start_hash"`
	BlockHash                string            `json:"block_hash"`
	BlockTxs                 int               `json:"block_txs"`
	FinalizeTxs              int               `json:"finalize_txs"`
	BlobTxsUnwrapped         int               `json:"blob_txs_unwrapped"`
	NextBlockExpectedAppHash string            `json:"next_block_expected_app_hash,omitempty"`
	CheckTx                  []txResultSummary `json:"check_tx,omitempty"`
	ProcessProposal          *proposalSummary  `json:"process_proposal,omitempty"`
	FinalizeBlock            finalizeSummary   `json:"finalize_block"`
	Commit                   *commitSummary    `json:"commit,omitempty"`
	StoreHashes              []storeHash       `json:"store_hashes,omitempty"`
}

type txResultSummary struct {
	Index     int    `json:"index"`
	Code      uint32 `json:"code"`
	Codespace string `json:"codespace,omitempty"`
	Log       string `json:"log,omitempty"`
	GasWanted int64  `json:"gas_wanted"`
	GasUsed   int64  `json:"gas_used"`
}

type proposalSummary struct {
	Status   string `json:"status"`
	Accepted bool   `json:"accepted"`
}

type finalizeSummary struct {
	AppHash             string            `json:"app_hash"`
	MatchesNextBlock    *bool             `json:"matches_next_block,omitempty"`
	TxResults           []txResultSummary `json:"tx_results,omitempty"`
	ValidatorUpdates    int               `json:"validator_updates"`
	ConsensusParamDelta bool              `json:"consensus_param_delta"`
}

type commitSummary struct {
	RetainHeight   int64  `json:"retain_height"`
	AppEndHeight   int64  `json:"app_end_height"`
	AppEndHash     string `json:"app_end_hash"`
	CommitInfoHash string `json:"commit_info_hash,omitempty"`
}

type storeHash struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

func (r *blockRunner) run() (*runBlockResult, error) {
	if r.height <= 0 {
		return nil, fmt.Errorf("--%s must be set to the block height to execute", flagHeight)
	}
	if err := validateRunBlockMode(r.mode); err != nil {
		return nil, err
	}

	dataDir := filepath.Join(r.home, "data")
	appBackend := server.GetAppDBBackend(r.serverCtx.Viper)
	appDB, err := dbm.NewDB("application", appBackend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open application db: %w", err)
	}
	appOwnsDB := false
	defer func() {
		if !appOwnsDB {
			_ = appDB.Close()
		}
	}()

	cmtBackend := cmtdbm.BackendType(r.serverCtx.Config.DBBackend)
	blockDB, err := cmtdbm.NewDB("blockstore", cmtBackend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open blockstore db: %w", err)
	}
	defer blockDB.Close()
	stateDB, err := cmtdbm.NewDB("state", cmtBackend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	defer stateDB.Close()

	blockStore := store.NewBlockStore(blockDB)
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{DiscardABCIResponses: false})
	cometState, err := stateStore.Load()
	if err != nil {
		return nil, fmt.Errorf("load comet state: %w", err)
	}
	block := blockStore.LoadBlock(r.height)
	if block == nil {
		return nil, fmt.Errorf("block %d not found in blockstore", r.height)
	}

	application := NewAppServer(r.serverCtx.Logger, appDB, nil, r.serverCtx.Viper)
	appOwnsDB = true
	defer application.Close()
	info, err := application.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, fmt.Errorf("app info: %w", err)
	}

	if info.LastBlockHeight != r.height-1 {
		return nil, fmt.Errorf("app state is at height %d; run-block height %d requires app state at height %d", info.LastBlockHeight, r.height, r.height-1)
	}

	rawTxs := block.Data.Txs.ToSliceOfBytes()
	finalizeTxs, blobTxsUnwrapped := unwrapBlobTxsForFinalize(block)
	commitInfo, err := lastCommitInfo(stateStore, block, cometState.InitialHeight)
	if err != nil {
		return nil, err
	}
	expectedAppHash := nextBlockExpectedAppHash(blockStore, r.height)

	result := &runBlockResult{
		Home:                     r.home,
		Mode:                     r.mode,
		Committed:                r.commit,
		Height:                   r.height,
		CometLastBlockHeight:     cometState.LastBlockHeight,
		AppStartHeight:           info.LastBlockHeight,
		AppStartHash:             fmt.Sprintf("%X", info.LastBlockAppHash),
		BlockHash:                fmt.Sprintf("%X", block.Hash()),
		BlockTxs:                 len(rawTxs),
		FinalizeTxs:              len(finalizeTxs),
		BlobTxsUnwrapped:         blobTxsUnwrapped,
		NextBlockExpectedAppHash: expectedAppHash,
	}

	if r.mode == runBlockModeCheckProcessFinalize {
		checkResults, err := runCheckTx(application, rawTxs)
		if err != nil {
			return nil, err
		}
		result.CheckTx = checkResults
	}

	if r.mode == runBlockModeProcessFinalize || r.mode == runBlockModeCheckProcessFinalize {
		processResp, err := application.ProcessProposal(processProposalRequest(block, rawTxs, commitInfo))
		if err != nil {
			return nil, fmt.Errorf("process proposal: %w", err)
		}
		if processResp.IsStatusUnknown() {
			return nil, fmt.Errorf("process proposal returned unknown status")
		}
		result.ProcessProposal = &proposalSummary{
			Status:   processResp.Status.String(),
			Accepted: processResp.IsAccepted(),
		}
		if !processResp.IsAccepted() {
			return result, nil
		}
	}

	finalizeResp, err := application.FinalizeBlock(finalizeBlockRequest(block, finalizeTxs, commitInfo))
	if err != nil {
		return nil, fmt.Errorf("finalize block: %w", err)
	}
	finalizeHash := fmt.Sprintf("%X", finalizeResp.AppHash)
	result.FinalizeBlock = finalizeSummary{
		AppHash:             finalizeHash,
		TxResults:           summarizeExecTxResults(finalizeResp.TxResults),
		ValidatorUpdates:    len(finalizeResp.ValidatorUpdates),
		ConsensusParamDelta: finalizeResp.ConsensusParamUpdates != nil,
	}
	if expectedAppHash != "" {
		matches := strings.EqualFold(finalizeHash, expectedAppHash)
		result.FinalizeBlock.MatchesNextBlock = &matches
	}

	if r.commit {
		commitResp, err := application.Commit()
		if err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
		endInfo, err := application.Info(&abci.RequestInfo{})
		if err != nil {
			return nil, fmt.Errorf("app info after commit: %w", err)
		}
		result.Commit = &commitSummary{
			RetainHeight: commitResp.RetainHeight,
			AppEndHeight: endInfo.LastBlockHeight,
			AppEndHash:   fmt.Sprintf("%X", endInfo.LastBlockAppHash),
		}
		stores, commitHash, err := appStoreHashes(application.CommitMultiStore(), r.height)
		if err != nil {
			return nil, err
		}
		result.Commit.CommitInfoHash = commitHash
		result.StoreHashes = stores
	}

	return result, nil
}

func validateRunBlockMode(mode string) error {
	switch mode {
	case runBlockModeFinalizeOnly, runBlockModeProcessFinalize, runBlockModeCheckProcessFinalize:
		return nil
	default:
		return fmt.Errorf("unknown --%s %q; expected %q, %q, or %q", flagRunBlockMode, mode, runBlockModeFinalizeOnly, runBlockModeProcessFinalize, runBlockModeCheckProcessFinalize)
	}
}

func lastCommitInfo(stateStore sm.Store, block *cmttypes.Block, initialHeight int64) (abci.CommitInfo, error) {
	if block.Height == initialHeight {
		return abci.CommitInfo{}, nil
	}
	lastValSet, err := stateStore.LoadValidators(block.Height - 1)
	if err != nil {
		return abci.CommitInfo{}, fmt.Errorf("load validator set at height %d: %w", block.Height-1, err)
	}
	return sm.BuildLastCommitInfo(block, lastValSet, initialHeight), nil
}

func processProposalRequest(block *cmttypes.Block, txs [][]byte, commitInfo abci.CommitInfo) *abci.RequestProcessProposal {
	pbHeader := block.Header.ToProto()
	return &abci.RequestProcessProposal{
		Hash:               block.Header.Hash(),
		Height:             block.Header.Height,
		Time:               block.Header.Time,
		Txs:                txs,
		SquareSize:         block.Data.SquareSize,
		DataRootHash:       block.Data.Hash(),
		ProposedLastCommit: commitInfo,
		Misbehavior:        block.Evidence.Evidence.ToABCI(),
		ProposerAddress:    block.ProposerAddress,
		NextValidatorsHash: block.NextValidatorsHash,
		Header:             pbHeader,
	}
}

func finalizeBlockRequest(block *cmttypes.Block, txs [][]byte, commitInfo abci.CommitInfo) *abci.RequestFinalizeBlock {
	pbHeader := block.Header.ToProto()
	return &abci.RequestFinalizeBlock{
		Hash:               block.Hash(),
		NextValidatorsHash: block.NextValidatorsHash,
		ProposerAddress:    block.ProposerAddress,
		Height:             block.Height,
		Time:               block.Time,
		DecidedLastCommit:  commitInfo,
		Misbehavior:        block.Evidence.Evidence.ToABCI(),
		Txs:                txs,
		Header:             pbHeader,
	}
}

func unwrapBlobTxsForFinalize(block *cmttypes.Block) ([][]byte, int) {
	txs := make([][]byte, len(block.Txs))
	unwrapped := 0
	for i, tx := range block.Txs {
		blobTx, isBlobTx := cmttypes.UnmarshalBlobTx(tx)
		if isBlobTx {
			tx = blobTx.Tx
			unwrapped++
		}
		txs[i] = tx
	}
	return txs, unwrapped
}

func runCheckTx(application interface {
	CheckTx(*abci.RequestCheckTx) (*abci.ResponseCheckTx, error)
}, txs [][]byte) ([]txResultSummary, error) {
	results := make([]txResultSummary, len(txs))
	for i, tx := range txs {
		resp, err := application.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
		if err != nil {
			return nil, fmt.Errorf("check tx %d: %w", i, err)
		}
		results[i] = txResultSummary{
			Index:     i,
			Code:      resp.Code,
			Codespace: resp.Codespace,
			Log:       resp.Log,
			GasWanted: resp.GasWanted,
			GasUsed:   resp.GasUsed,
		}
		if resp.IsErr() {
			return results, fmt.Errorf("check tx %d failed: code=%d codespace=%s log=%s", i, resp.Code, resp.Codespace, resp.Log)
		}
	}
	return results, nil
}

func summarizeExecTxResults(results []*abci.ExecTxResult) []txResultSummary {
	out := make([]txResultSummary, len(results))
	for i, result := range results {
		out[i] = txResultSummary{
			Index:     i,
			Code:      result.Code,
			Codespace: result.Codespace,
			Log:       result.Log,
			GasWanted: result.GasWanted,
			GasUsed:   result.GasUsed,
		}
	}
	return out
}

func nextBlockExpectedAppHash(blockStore *store.BlockStore, height int64) string {
	next := blockStore.LoadBlock(height + 1)
	if next == nil {
		return ""
	}
	return fmt.Sprintf("%X", next.AppHash)
}

func appStoreHashes(cms storetypes.CommitMultiStore, height int64) ([]storeHash, string, error) {
	getter, ok := cms.(interface {
		GetCommitInfo(int64) (*storetypes.CommitInfo, error)
	})
	if !ok {
		return nil, "", fmt.Errorf("commit multistore does not expose GetCommitInfo")
	}
	ci, err := getter.GetCommitInfo(height)
	if err != nil {
		return nil, "", fmt.Errorf("get app commit info at height %d: %w", height, err)
	}
	stores := make([]storeHash, len(ci.StoreInfos))
	for i, si := range ci.StoreInfos {
		stores[i] = storeHash{
			Name: si.Name,
			Hash: fmt.Sprintf("%X", si.CommitId.Hash),
		}
	}
	return stores, fmt.Sprintf("%X", ci.Hash()), nil
}
