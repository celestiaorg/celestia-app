package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"

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
)

var defaultNamespace share.Namespace

// emptyBlockData contains the protobuf block data for a block without transactions.
var emptyBlockData = func() tmproto.Data {
	dataSquare, txs, err := square.Build(
		[][]byte{},
		maxSquareSize,
		appconsts.SubtreeRootThreshold(1),
	)
	if err != nil {
		panic(err)
	}

	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	if err != nil {
		panic(err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		panic(err)
	}
	return tmproto.Data{
		Txs:        txs,
		Hash:       dah.Hash(),
		SquareSize: uint64(dataSquare.Size()),
	}
}()

const (
	defaultNamespaceStr = "test"
	maxSquareSize       = 512
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

func Run(ctx context.Context, cfg BuilderConfig, dir string) error {
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
			return fmt.Errorf("failed to create keyring: %w", err)
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
			return fmt.Errorf("failed to initialize genesis files: %w", err)
		}
		fmt.Println("Creating chain from scratch with Chain ID:", gen.ChainID)
	} else {
		cfgPath := filepath.Join(cfg.ExistingDir, "config/config.toml")
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			return fmt.Errorf("config file for existing chain not found at %s", cfgPath)
		}
		fmt.Println("Loading chain from existing directory:", cfg.ExistingDir)
		tmCfg.SetRoot(cfg.ExistingDir)
		kr, err = keyring.New(app.Name, keyring.BackendTest, cfg.ExistingDir, nil, encCfg.Codec)
		if err != nil {
			return fmt.Errorf("failed to load keyring: %w", err)
		}
	}

	validatorKey := privval.LoadFilePV(tmCfg.PrivValidatorKeyFile(), tmCfg.PrivValidatorStateFile())
	validatorAddr := validatorKey.Key.Address

	blockDB, err := dbm.NewDB("blockstore", dbm.PebbleDBBackend, tmCfg.DBDir())
	if err != nil {
		return fmt.Errorf("failed to create block database: %w", err)
	}

	blockStore := store.NewBlockStore(blockDB)

	stateDB, err := dbm.NewDB("state", dbm.PebbleDBBackend, tmCfg.DBDir())
	if err != nil {
		return fmt.Errorf("failed to create state database: %w", err)
	}

	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: true,
	})

	appDB, err := tmdbm.NewDB("application", tmdbm.GoLevelDBBackend, tmCfg.DBDir())
	if err != nil {
		return fmt.Errorf("failed to create application database: %w", err)
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
		return fmt.Errorf("last application height is %d, but the block store height is %d", infoResp.LastBlockHeight, lastHeight)
	}

	if lastHeight == 0 {
		if gen == nil {
			return fmt.Errorf("non empty directory but no blocks found")
		}

		genDoc, err := gen.Export()
		if err != nil {
			return fmt.Errorf("failed to export genesis document: %w", err)
		}

		state, err := stateStore.LoadFromDBOrGenesisDoc(genDoc)
		if err != nil {
			return fmt.Errorf("failed to load state from database or genesis document: %w", err)
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
			return fmt.Errorf("failed to convert validator updates: %w", err)
		}
		state.Validators = types.NewValidatorSet(vals)
		state.NextValidators = types.NewValidatorSet(vals).CopyIncrementProposerPriority(1)
		state.AppHash = res.AppHash
		state.LastResultsHash = merkle.HashFromByteSlices(nil)
		if err := stateStore.Save(state); err != nil {
			return fmt.Errorf("failed to save initial state: %w", err)
		}
		currentTime = currentTime.Add(cfg.BlockInterval)
	} else {
		fmt.Println("Starting from height", lastHeight)
	}
	state, err := stateStore.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}
	if cfg.ExistingDir != "" {
		// if this is extending an existing chain, we want to start
		// the time to be where the existing chain left off
		currentTime = state.LastBlockTime.Add(cfg.BlockInterval)
	}

	if state.ConsensusParams.Version.AppVersion != cfg.AppVersion {
		return fmt.Errorf("app version mismatch: state has %d, but cfg has %d", state.ConsensusParams.Version.AppVersion, cfg.AppVersion)
	}

	if state.LastBlockHeight != lastHeight {
		return fmt.Errorf("last block height mismatch: state has %d, but block store has %d", state.LastBlockHeight, lastHeight)
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
		return fmt.Errorf("failed to create new signer: %w", err)
	}

	var (
		errCh     = make(chan error, 2)
		dataCh    = make(chan *tmproto.Data, 100)
		persistCh = make(chan persistData, 100)
		commit    = types.NewCommit(0, 0, types.BlockID{}, nil)
	)
	if lastHeight > 0 {
		commit = blockStore.LoadSeenCommit(lastHeight)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		errCh <- generateSquareRoutine(ctx, signer, cfg, dataCh)
	}()

	go func() {
		errCh <- persistDataRoutine(ctx, stateStore, blockStore, persistCh)
	}()

	lastBlock := blockStore.LoadBlock(blockStore.Height())

	for height := lastHeight + 1; height <= int64(cfg.NumBlocks)+lastHeight; height++ {
		if cfg.UpToTime && lastBlock != nil && lastBlock.Time.Add(cfg.BlockInterval).After(time.Now().UTC()) {
			fmt.Printf("blocks cannot be generated into the future, stopping at height %d\n", lastBlock.Height)
			break
		}

		var dd *tmproto.Data
		if height == 1 {
			// generating an empty block for height 1
			dd = &emptyBlockData
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case dataPB := <-dataCh:
				dd = dataPB
			}
		}

		data, err := types.DataFromProto(dd)
		if err != nil {
			return fmt.Errorf("failed to convert data from protobuf: %w", err)
		}
		block, blockParts := state.MakeBlock(height, data, commit, nil, validatorAddr)
		blockID := types.BlockID{
			Hash:          block.Hash(),
			PartSetHeader: blockParts.Header(),
		}

		precommitVote := &tmproto.Vote{
			Height:           height,
			Round:            0,
			Type:             tmproto.PrecommitType,
			BlockID:          blockID.ToProto(),
			ValidatorAddress: validatorAddr,
			Timestamp:        currentTime,
			Signature:        nil,
		}

		if err := validatorKey.SignVote(state.ChainID, precommitVote); err != nil {
			return fmt.Errorf("failed to sign precommit vote (%s): %w", precommitVote.String(), err)
		}

		commitSig := types.CommitSig{
			BlockIDFlag:      types.BlockIDFlagCommit,
			ValidatorAddress: validatorAddr,
			Timestamp:        currentTime,
			Signature:        precommitVote.Signature,
		}
		commit = types.NewCommit(height, 0, blockID, []types.CommitSig{commitSig})

		var lastCommitInfo abci.LastCommitInfo
		if height > 1 {
			lastCommitInfo = abci.LastCommitInfo{
				Round: 0,
				Votes: []abci.VoteInfo{
					{
						Validator: abci.Validator{
							Address: validatorAddr,
							Power:   validatorPower,
						},
						SignedLastBlock: true,
					},
				},
			}
		}

		beginBlockResp := simApp.BeginBlock(abci.RequestBeginBlock{
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
			deliverTxResponse := simApp.DeliverTx(abci.RequestDeliverTx{
				Tx: tx,
			})
			if deliverTxResponse.Code != abci.CodeTypeOK {
				return fmt.Errorf("failed to deliver tx: %s", deliverTxResponse.Log)
			}
			deliverTxResponses[idx] = &deliverTxResponse
		}

		endBlockResp := simApp.EndBlock(abci.RequestEndBlock{
			Height: block.Height,
		})

		commitResp := simApp.Commit()
		state.LastBlockHeight = height
		state.LastBlockID = blockID
		state.LastBlockTime = block.Time
		state.LastValidators = state.Validators
		state.Validators = state.NextValidators
		state.NextValidators = state.NextValidators.CopyIncrementProposerPriority(1)
		state.AppHash = commitResp.Data
		state.LastResultsHash = sm.ABCIResponsesResultsHash(&smproto.ABCIResponses{
			DeliverTxs: deliverTxResponses,
			BeginBlock: &beginBlockResp,
			EndBlock:   &endBlockResp,
		})
		currentTime = currentTime.Add(cfg.BlockInterval)
		persistCh <- persistData{
			state: state.Copy(),
			block: block,
			seenCommit: &types.Commit{
				Height:     commit.Height,
				Round:      commit.Round,
				BlockID:    commit.BlockID,
				Signatures: []types.CommitSig{commitSig},
			},
		}
	}

	close(dataCh)
	close(persistCh)

	var firstErr error
	for i := 0; i < cap(errCh); i++ {
		err := <-errCh
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if err := blockDB.Close(); err != nil {
		return fmt.Errorf("failed to close block database: %w", err)
	}
	if err := stateDB.Close(); err != nil {
		return fmt.Errorf("failed to close state database: %w", err)
	}
	if err := appDB.Close(); err != nil {
		return fmt.Errorf("failed to close application database: %w", err)
	}

	fmt.Println("Chain built successfully", state.LastBlockHeight)

	return firstErr
}

func generateSquareRoutine(
	ctx context.Context,
	signer *user.Signer,
	cfg BuilderConfig,
	dataCh chan<- *tmproto.Data,
) error {
	// cfg.NumBlocks-1 because block 0 is genesis and block 1 shouldn't contain any transaction
	for i := 0; i < cfg.NumBlocks-1; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		account := signer.Accounts()[0]

		blobTxs := make([][]byte, 0)
		numberOfBlobs := rand.Intn(100) + 1
		blobSize := cfg.BlockSize / numberOfBlobs
		for size := 0; size < cfg.BlockSize; size += blobSize {
			blob, err := share.NewV0Blob(share.RandomNamespace(), crypto.CRandBytes(blobSize))
			if err != nil {
				return err
			}

			blobGas := blobtypes.DefaultEstimateGas([]uint32{uint32(blobSize)})
			fee := float64(blobGas) * appconsts.DefaultMinGasPrice * 2
			tx, _, err := signer.CreatePayForBlobs(account.Name(), []*share.Blob{blob}, user.SetGasLimit(blobGas), user.SetFee(uint64(fee)))
			if err != nil {
				return err
			}
			if err := signer.IncrementSequence(account.Name()); err != nil {
				return err
			}
			blobTxs = append(blobTxs, tx)
		}

		dataSquare, txs, err := square.Build(
			blobTxs,
			maxSquareSize,
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

		select {
		case dataCh <- &tmproto.Data{
			Txs:        txs,
			Hash:       dah.Hash(),
			SquareSize: uint64(dataSquare.Size()),
		}:
		case <-ctx.Done():
			return ctx.Err()
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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data, ok := <-dataCh:
			if !ok {
				return nil
			}
			blockParts := data.block.MakePartSet(types.BlockPartSizeBytes)
			blockStore.SaveBlock(data.block, blockParts, data.seenCommit)
			if blockStore.Height()%100 == 0 {
				fmt.Println("Reached height", blockStore.Height())
			}

			if err := stateStore.Save(data.state); err != nil {
				return err
			}
		}
	}
}
