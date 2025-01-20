package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	dbm "github.com/cometbft/cometbft-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/libs/log"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/privval"
	smproto "github.com/tendermint/tendermint/proto/tendermint/state"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"
	"github.com/tendermint/tendermint/types"
	tmdbm "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
)

var defaultNamespace share.Namespace

const (
	defaultNamespaceStr = "test"
)

func init() {
	defaultNamespace = share.MustNewV0Namespace([]byte(defaultNamespaceStr))
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "chainbuilder",
		Short: "Build a Celestia chain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			numBlocks, _ := cmd.Flags().GetInt("num-blocks")
			blockSize, _ := cmd.Flags().GetInt("block-size")
			blockInterval, _ := cmd.Flags().GetDuration("block-interval")
			existingDir, _ := cmd.Flags().GetString("existing-dir")
			namespaceStr, _ := cmd.Flags().GetString("namespace")
			upToTime, _ := cmd.Flags().GetBool("up-to-now")
			appVersion, _ := cmd.Flags().GetUint64("app-version")
			chainID, _ := cmd.Flags().GetString("chain-id")
			var namespace share.Namespace
			if namespaceStr == "" {
				namespace = defaultNamespace
			} else {
				var err error
				namespace, err = share.NewV0Namespace([]byte(namespaceStr))
				if err != nil {
					return fmt.Errorf("invalid namespace: %w", err)
				}
			}

			cfg := BuilderConfig{
				NumBlocks:     numBlocks,
				BlockSize:     blockSize,
				BlockInterval: blockInterval,
				ExistingDir:   existingDir,
				Namespace:     namespace,
				ChainID:       tmrand.Str(6),
				UpToTime:      upToTime,
				AppVersion:    appVersion,
			}

			if chainID != "" {
				cfg.ChainID = chainID
			}

			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}

			return Run(cmd.Context(), cfg, dir)
		},
	}

	rootCmd.Flags().Int("num-blocks", 100, "Number of blocks to generate")
	rootCmd.Flags().Int("block-size", appconsts.DefaultMaxBytes, "Size of each block in bytes")
	rootCmd.Flags().Duration("block-interval", time.Second, "Interval between blocks")
	rootCmd.Flags().String("existing-dir", "", "Existing directory to load chain from")
	rootCmd.Flags().String("namespace", "", "Custom namespace for the chain")
	rootCmd.Flags().Bool("up-to-now", false, "Tool will terminate if the block time reaches the current time")
	rootCmd.Flags().Uint64("app-version", appconsts.LatestVersion, "App version to use for the chain")
	rootCmd.Flags().String("chain-id", "", "Chain ID to use for the chain. Defaults to a random 6 character string")
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func Run(ctx context.Context, cfg BuilderConfig, dir string) error {
	var (
		errCh     = make(chan error, 3)
		dataCh    = make(chan *tmproto.Data, 10)
		persistCh = make(chan persistData, 10)
		wg        = &sync.WaitGroup{}
	)

	// Create a cancelable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Capture system signals (e.g., Ctrl+C)
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		fmt.Println("Received interrupt signal. Shutting down gracefully...")
		cancel()
	}()

	cb, err := NewChainBuilder(ctx, cfg, dir)
	if err != nil {
		return fmt.Errorf("failed to create chain builder: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- generateSquareRoutine(ctx, cb.signer, cfg, dataCh)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- createBlock(cb, dataCh, persistCh)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- persistDataRoutine(ctx, cb.stateStore, cb.blockStore, persistCh)
	}()

	var firstErr error

	go func() {
		for err := range errCh {
			if err != nil {
				firstErr = err
				cancel()
			}
		}
	}()

	wg.Wait()
	err = cb.Close()
	if err != nil {
		return fmt.Errorf("failed to close chain builder: %w %w", err, firstErr)
	}
	return firstErr
}

type BuilderConfig struct {
	NumBlocks     int
	BlockSize     int
	BlockInterval time.Duration
	ExistingDir   string
	Namespace     share.Namespace
	ChainID       string
	AppVersion    uint64
	UpToTime      bool
}

type ChainBuilder struct {
	cfg         BuilderConfig
	blockStore  *store.BlockStore
	stateStore  sm.Store
	appDB       tmdbm.DB
	app         *app.App
	signer      *user.Signer
	state       sm.State
	startTime   time.Time
	currentTime time.Time
	proposer    []byte
	valKey      *privval.FilePV
	valPower    int64
}

func (cb ChainBuilder) Close() error {
	if err := cb.blockStore.Close(); err != nil {
		return fmt.Errorf("failed to close block database: %w", err)
	}
	if err := cb.stateStore.Close(); err != nil {
		return fmt.Errorf("failed to close state database: %w", err)
	}
	if err := cb.app.Close(); err != nil {
		return fmt.Errorf("failed to close application: %w", err)
	}
	if err := cb.appDB.Close(); err != nil {
		return fmt.Errorf("failed to close application database: %w", err)
	}
	return nil
}

func NewChainBuilder(ctx context.Context, cfg BuilderConfig, dir string) (ChainBuilder, error) {
	startTime := time.Now().Add(-1 * cfg.BlockInterval * time.Duration(cfg.NumBlocks)).UTC()
	currentTime := startTime

	encCfg := encoding.MakeConfig(app.ModuleBasics)
	tmCfg := app.DefaultConsensusConfig()
	var (
		gen *genesis.Genesis
		kr  keyring.Keyring
		err error
	)
	if cfg.ExistingDir == "" {
		dir = filepath.Join(dir, fmt.Sprintf("testnode-%s", cfg.ChainID))
		kr, err = keyring.New(app.Name, keyring.BackendTest, dir, nil, encCfg.Codec)
		if err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to create keyring: %w", err)
		}

		validator := genesis.NewDefaultValidator(testnode.DefaultValidatorAccountName)
		appCfg := app.DefaultAppConfig()
		appCfg.Pruning = "everything" // we just want the last two states
		appCfg.StateSync.SnapshotInterval = 0
		cp := app.DefaultConsensusParams()

		cp.Version.AppVersion = cfg.AppVersion // set the app version
		gen = genesis.NewDefaultGenesis().
			WithConsensusParams(cp).
			WithKeyring(kr).
			WithChainID(cfg.ChainID).
			WithGenesisTime(startTime).
			WithValidators(validator)

		if err := genesis.InitFiles(dir, tmCfg, appCfg, gen, 0); err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to initialize genesis files: %w", err)
		}
		fmt.Println("Creating chain from scratch with Chain ID:", gen.ChainID)
	} else {
		cfgPath := filepath.Join(cfg.ExistingDir, "config/config.toml")
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			return ChainBuilder{}, fmt.Errorf("config file for existing chain not found at %s", cfgPath)
		}
		fmt.Println("Loading chain from existing directory:", cfg.ExistingDir)
		tmCfg.SetRoot(cfg.ExistingDir)
		kr, err = keyring.New(app.Name, keyring.BackendTest, cfg.ExistingDir, nil, encCfg.Codec)
		if err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to load keyring: %w", err)
		}
	}

	validatorKey := privval.LoadFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile())
	validatorAddr := validatorKey.Key.Address

	blockDB, err := dbm.NewDB("blockstore", dbm.GoLevelDBBackend, tmCfg.DBDir())
	if err != nil {
		return ChainBuilder{}, fmt.Errorf("failed to create block database: %w", err)
	}

	blockStore := store.NewBlockStore(blockDB)

	stateDB, err := dbm.NewDB("state", dbm.GoLevelDBBackend, tmCfg.DBDir())
	if err != nil {
		return ChainBuilder{}, fmt.Errorf("failed to create state database: %w", err)
	}

	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: true,
	})

	appDB, err := tmdbm.NewDB("application", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	if err != nil {
		return ChainBuilder{}, fmt.Errorf("failed to create application database: %w", err)
	}

	simApp := app.New(
		log.NewNopLogger(),
		appDB,
		nil,
		0,
		encCfg,
		0, // upgrade height v2
		0, // timeout commit
		util.EmptyAppOptions{},
		baseapp.SetMinGasPrices(fmt.Sprintf("%f%s", appconsts.DefaultMinGasPrice, appconsts.BondDenom)),
	)

	infoResp := simApp.Info(abci.RequestInfo{})

	lastHeight := blockStore.Height()
	if infoResp.LastBlockHeight != lastHeight {
		return ChainBuilder{}, fmt.Errorf("last application height is %d, but the block store height is %d", infoResp.LastBlockHeight, lastHeight)
	}

	if lastHeight == 0 {
		if gen == nil {
			return ChainBuilder{}, fmt.Errorf("non empty directory but no blocks found")
		}

		genDoc, err := gen.Export()
		if err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to export genesis document: %w", err)
		}

		state, err := stateStore.LoadFromDBOrGenesisDoc(genDoc)
		if err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to load state from database or genesis document: %w", err)
		}

		validators := make([]*types.Validator, len(genDoc.Validators))
		for i, val := range genDoc.Validators {
			validators[i] = types.NewValidator(val.PubKey, val.Power)
		}
		validatorSet := types.NewValidatorSet(validators)
		nextVals := types.TM2PB.ValidatorUpdates(validatorSet)
		csParams := types.TM2PB.ConsensusParams(genDoc.ConsensusParams)
		res := simApp.InitChain(abci.RequestInitChain{
			ChainId:         genDoc.ChainID,
			Time:            genDoc.GenesisTime,
			ConsensusParams: csParams,
			Validators:      nextVals,
			AppStateBytes:   genDoc.AppState,
			InitialHeight:   genDoc.InitialHeight,
		})

		vals, err := types.PB2TM.ValidatorUpdates(res.Validators)
		if err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to convert validator updates: %w", err)
		}
		state.Validators = types.NewValidatorSet(vals)
		state.NextValidators = types.NewValidatorSet(vals).CopyIncrementProposerPriority(1)
		state.AppHash = res.AppHash
		state.LastResultsHash = merkle.HashFromByteSlices(nil)
		if err := stateStore.Save(state); err != nil {
			return ChainBuilder{}, fmt.Errorf("failed to save initial state: %w", err)
		}
		currentTime = currentTime.Add(cfg.BlockInterval)
	} else {
		fmt.Println("Starting from height", lastHeight)
	}
	state, err := stateStore.Load()
	if err != nil {
		return ChainBuilder{}, fmt.Errorf("failed to load state: %w", err)
	}
	if cfg.ExistingDir != "" {
		// if this is extending an existing chain, we want to start
		// the time to be where the existing chain left off
		currentTime = state.LastBlockTime.Add(cfg.BlockInterval)
	}

	if state.ConsensusParams.Version.AppVersion != cfg.AppVersion {
		return ChainBuilder{}, fmt.Errorf("app version mismatch: state has %d, but cfg has %d", state.ConsensusParams.Version.AppVersion, cfg.AppVersion)
	}

	if state.LastBlockHeight != lastHeight {
		return ChainBuilder{}, fmt.Errorf("last block height mismatch: state has %d, but block store has %d", state.LastBlockHeight, lastHeight)
	}

	validatorPower := state.Validators.Validators[0].VotingPower

	signer, err := user.NewSigner(
		kr,
		encCfg.TxConfig,
		state.ChainID,
		state.ConsensusParams.Version.AppVersion,
		user.NewAccount(testnode.DefaultValidatorAccountName, 0, uint64(lastHeight)+1),
	)
	if err != nil {
		return ChainBuilder{}, fmt.Errorf("failed to create new signer: %w", err)
	}

	return ChainBuilder{
		cfg:         cfg,
		blockStore:  blockStore,
		stateStore:  stateStore,
		appDB:       appDB,
		app:         simApp,
		signer:      signer,
		startTime:   startTime,
		currentTime: currentTime,
		state:       state,
		proposer:    validatorAddr,
		valKey:      validatorKey,
		valPower:    validatorPower,
	}, nil
}

func createBlock(cb ChainBuilder, dataCh chan *tmproto.Data, persistCh chan<- persistData) error {
	var (
		commit = types.NewCommit(0, 0, types.BlockID{}, nil)
	)

	lastHeight := cb.blockStore.Height()
	if lastHeight > 0 {
		commit = cb.blockStore.LoadSeenCommit(lastHeight)
	}

	defer close(persistCh)
	for dataPB := range dataCh {
		lastHeight++
		data, err := types.DataFromProto(dataPB)
		if err != nil {
			return fmt.Errorf("failed to convert data from protobuf: %w", err)
		}

		block, blockParts := cb.state.MakeBlock(lastHeight, data, commit, nil, cb.proposer)
		blockID := types.BlockID{
			Hash:          block.Hash(),
			PartSetHeader: blockParts.Header(),
		}

		precommitVote := &tmproto.Vote{
			Height:           lastHeight,
			Round:            0,
			Type:             tmproto.PrecommitType,
			BlockID:          blockID.ToProto(),
			ValidatorAddress: cb.proposer,
			Timestamp:        cb.startTime,
			Signature:        nil,
		}

		if err := cb.valKey.SignVote(cb.state.ChainID, precommitVote); err != nil {
			return fmt.Errorf("failed to sign precommit vote (%s): %w", precommitVote.String(), err)
		}

		commitSig := types.CommitSig{
			BlockIDFlag:      types.BlockIDFlagCommit,
			ValidatorAddress: cb.proposer,
			Timestamp:        cb.currentTime,
			Signature:        precommitVote.Signature,
		}
		commit = types.NewCommit(lastHeight, 0, blockID, []types.CommitSig{commitSig})

		var lastCommitInfo abci.LastCommitInfo
		if lastHeight > 1 {
			lastCommitInfo = abci.LastCommitInfo{
				Round: 0,
				Votes: []abci.VoteInfo{
					{
						Validator: abci.Validator{
							Address: cb.proposer,
							Power:   cb.valPower,
						},
						SignedLastBlock: true,
					},
				},
			}
		}

		beginBlockResp := cb.app.BeginBlock(abci.RequestBeginBlock{
			Hash:           block.Hash(),
			Header:         *block.Header.ToProto(),
			LastCommitInfo: lastCommitInfo,
		})

		deliverTxResponses := make([]*abci.ResponseDeliverTx, len(block.Data.Txs))

		for idx, tx := range block.Data.Txs {
			blobTx, isBlobTx := types.UnmarshalBlobTx(tx)
			if isBlobTx {
				tx = blobTx.Tx
			}
			deliverTxResponse := cb.app.DeliverTx(abci.RequestDeliverTx{
				Tx: tx,
			})
			if deliverTxResponse.Code != abci.CodeTypeOK {
				return fmt.Errorf("failed to deliver tx: %s", deliverTxResponse.Log)
			}
			deliverTxResponses[idx] = &deliverTxResponse
		}

		endBlockResp := cb.app.EndBlock(abci.RequestEndBlock{
			Height: block.Height,
		})

		commitResp := cb.app.Commit()
		cb.state.LastBlockHeight = lastHeight
		cb.state.LastBlockID = blockID
		cb.state.LastBlockTime = block.Time
		cb.state.LastValidators = cb.state.Validators
		cb.state.Validators = cb.state.NextValidators
		cb.state.NextValidators = cb.state.NextValidators.CopyIncrementProposerPriority(1)
		cb.state.AppHash = commitResp.Data
		cb.state.LastResultsHash = sm.ABCIResponsesResultsHash(&smproto.ABCIResponses{
			DeliverTxs: deliverTxResponses,
			BeginBlock: &beginBlockResp,
			EndBlock:   &endBlockResp,
		})
		cb.currentTime = cb.currentTime.Add(cb.cfg.BlockInterval)
		persistCh <- persistData{
			state: cb.state.Copy(),
			block: block,
			seenCommit: &types.Commit{
				Height:     commit.Height,
				Round:      commit.Round,
				BlockID:    commit.BlockID,
				Signatures: []types.CommitSig{commitSig},
			},
		}
	}

	return nil
}

func generateSquareRoutine(
	ctx context.Context,
	signer *user.Signer,
	cfg BuilderConfig,
	dataCh chan<- *tmproto.Data,
) error {
	defer close(dataCh)
	for i := 0; i < cfg.NumBlocks; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		account := signer.Accounts()[0]

		blobCount := cfg.BlockSize / (appconsts.MaxTxSize(cfg.AppVersion) + 100000)
		if blobCount == 0 {
			blobCount = 1
		}
		blobs := make([]*share.Blob, blobCount)
		for i := 0; i < blobCount; i++ {
			blob, err := share.NewV0Blob(cfg.Namespace, crypto.CRandBytes((appconsts.MaxTxSize(cfg.AppVersion))-100000))
			if err != nil {
				return err
			}
			blobs[i] = blob
		}

		blobGas := blobtypes.DefaultEstimateGas([]uint32{uint32(cfg.BlockSize)}) * 2
		fee := float64(blobGas) * appconsts.DefaultMinGasPrice * 2
		tx, _, err := signer.CreatePayForBlobs(account.Name(), blobs, user.SetGasLimit(blobGas), user.SetFee(uint64(fee)))
		if err != nil {
			return err
		}
		if err := signer.IncrementSequence(account.Name()); err != nil {
			return err
		}

		dataSquare, txs, err := square.Build(
			[][]byte{tx},
			512,
			appconsts.SubtreeRootThreshold(1),
		)
		if err != nil {
			return err
		}

		eds, err := da.ExtendShares(share.ToBytes(dataSquare))
		if err != nil {
			return err
		}

		dah, err := da.NewDataAvailabilityHeader(eds)
		if err != nil {
			return err
		}

		dataCh <- &tmproto.Data{
			Txs:        txs,
			Hash:       dah.Hash(),
			SquareSize: uint64(dataSquare.Size()),
		}
	}
	return nil
}

type persistData struct {
	state      sm.State
	block      *types.Block
	seenCommit *types.Commit
}

func persistDataRoutine(
	ctx context.Context,
	stateStore sm.Store,
	blockStore *store.BlockStore,
	dataCh <-chan persistData,
) error {
	for data := range dataCh {
		blockParts := data.block.MakePartSet(types.BlockPartSizeBytes)
		blockStore.SaveBlock(data.block, blockParts, data.seenCommit)
		if blockStore.Height()%100 == 0 {
			fmt.Println("Reached height", blockStore.Height())
		}

		if err := stateStore.Save(data.state); err != nil {
			return err
		}
	}
	return nil
}
