package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/go-square/v3/share"
	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/abci/types"
	abcimocks "github.com/cometbft/cometbft/abci/types/mocks"
	propagationtypes "github.com/cometbft/cometbft/consensus/propagation/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/log"
	mpmocks "github.com/cometbft/cometbft/mempool/mocks"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proxy"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/state/mocks"
	"github.com/cometbft/cometbft/store"
	comettypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/mock"
	"os"
	"runtime"
	"strings"
	"time"
)

// Reference times in milliseconds (to be filled with actual values)
const (
	referencePrepareProposalMs = 0.0
	referenceProcessProposalMs = 0.0
	referenceFinalizeBlockMs   = 0.0
	referenceProposeBlockMs    = 0.0
	referenceEncodeBlockMs     = 0.0
	referenceDecodeBlockMs     = 0.0
)

type CPUInfo struct {
	Cores      int
	Threads    int
	ClockSpeed string
	HasGFNI    bool
	HasSHANI   bool
}

func main() {
	if runtime.GOOS != "linux" {
		fmt.Println("Error: This script only runs on Linux")
		//os.Exit(1)
	}

	info, err := getCPUInfo()
	if err != nil {
		fmt.Printf("Error getting CPU info: %v\n", err)
		os.Exit(1)
	}

	displayCPUInfo(info)

	app, txs, err := generatePayForBlobTransactions(127, 1024*1024)
	if err != nil {
		fmt.Printf("Error generating transactions: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %d transactions of size %d\n", len(txs), len(txs[0]))

	prepareProposalTime, err := runPrepareProposal(app, txs)
	if err != nil {
		fmt.Printf("Error running PrepareProposal: %v\n", err)
		os.Exit(1)
	}
	processProposalTime, err := runProcessProposal(app, txs)
	if err != nil {
		fmt.Printf("Error running ProcessProposal: %v\n", err)
		os.Exit(1)
	}
	finalizeBlockTime, err := runFinalizeBlock(app, txs)
	if err != nil {
		fmt.Printf("Error running FinalizeBlock: %v\n", err)
		os.Exit(1)
	}
	proposeBlockTime, encodeBlockTime, decodeBlockTime, err := runProposeBlock(app, txs)
	if err != nil {
		fmt.Printf("Error running ProposeBlock: %v\n", err)
		os.Exit(1)
	}

	// Display performance results with comparison to reference times
	displayPerformanceResults(
		prepareProposalTime,
		processProposalTime,
		finalizeBlockTime,
		proposeBlockTime,
		encodeBlockTime,
		decodeBlockTime,
		info,
	)

	fmt.Println("\nDone")
}

func runPrepareProposal(testApp *app.App, txs [][]byte) (time.Duration, error) {
	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    txs,
		Height: testApp.LastBlockHeight() + 1,
	}

	start := time.Now()
	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	if err != nil {
		return 0, fmt.Errorf("error running PrepareProposal: %w", err)
	}
	if len(prepareProposalResp.Txs) == 0 {
		return 0, fmt.Errorf("no transactions returned")
	}
	elapsed := time.Since(start)
	return elapsed, nil
}

func runProcessProposal(testApp *app.App, txs [][]byte) (time.Duration, error) {
	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    txs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	if err != nil {
		return 0, fmt.Errorf("error running PrepareProposal: %w", err)
	}
	if len(prepareProposalResp.Txs) == 0 {
		return 0, fmt.Errorf("no transactions returned")
	}

	processProposalReq := types.RequestProcessProposal{
		Txs:          prepareProposalResp.Txs,
		Height:       testApp.LastBlockHeight() + 1,
		DataRootHash: prepareProposalResp.DataRootHash,
		SquareSize:   prepareProposalResp.SquareSize,
	}
	start := time.Now()
	_, err = testApp.ProcessProposal(&processProposalReq)
	if err != nil {
		return 0, fmt.Errorf("error running ProcessProposal: %w", err)
	}
	elapsed := time.Since(start)
	return elapsed, nil
}

func runFinalizeBlock(testApp *app.App, txs [][]byte) (time.Duration, error) {
	prepareProposalReq := types.RequestPrepareProposal{
		Txs:    txs,
		Height: testApp.LastBlockHeight() + 1,
	}

	prepareProposalResp, err := testApp.PrepareProposal(&prepareProposalReq)
	if err != nil {
		return 0, fmt.Errorf("error running PrepareProposal: %w", err)
	}
	if len(prepareProposalResp.Txs) == 0 {
		return 0, fmt.Errorf("no transactions returned")
	}

	finalizeBlockReq := types.RequestFinalizeBlock{
		Time:   testutil.GenesisTime.Add(time.Duration(6 * time.Second)),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    txs,
	}
	start := time.Now()
	_, err = testApp.FinalizeBlock(&finalizeBlockReq)
	if err != nil {
		return 0, fmt.Errorf("error running FinalizeBlock: %w", err)
	}
	elapsed := time.Since(start)
	return elapsed, nil
}

func runProposeBlock(testApp *app.App, txs [][]byte) (proposeBlockTime, encodeBlockTime, decodeBlockTime time.Duration, err error) {
	app := &abcimocks.Application{}
	app.On("PrepareProposal", mock.Anything, mock.Anything).Return(&types.ResponsePrepareProposal{
		Txs: txs,
	}, nil)
	cc := proxy.NewLocalClientCreator(app)
	proxyApp := proxy.NewAppConns(cc, proxy.NopMetrics())
	err = proxyApp.Start()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("error starting proxy app: %w", err)
	}
	defer proxyApp.Stop()

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.Anything).Return([]comettypes.Evidence{}, int64(0))

	cachedTxs := comettypes.CachedTxFromTxs(comettypes.ToTxs(txs))
	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything).Return(cachedTxs)

	nVals := 1
	vals := make([]comettypes.GenesisValidator, nVals)
	privVals := make(map[string]comettypes.PrivValidator, nVals)
	for i := 0; i < nVals; i++ {
		secret := []byte(fmt.Sprintf("test%d", i))
		pk := ed25519.GenPrivKeyFromSecret(secret)
		valAddr := pk.PubKey().Address()
		vals[i] = comettypes.GenesisValidator{
			Address: valAddr,
			PubKey:  pk.PubKey(),
			Power:   1000,
			Name:    fmt.Sprintf("test%d", i),
		}
		privVals[valAddr.String()] = comettypes.NewMockPVWithParams(pk, false, false)
	}
	genDoc := comettypes.GenesisDoc{
		ChainID:         testApp.ChainID(),
		Validators:      vals,
		AppHash:         nil,
		ConsensusParams: comettypes.DefaultConsensusParams(),
	}
	genDoc.ConsensusParams.Block.MaxBytes = comettypes.DefaultMaxBlockSizeBytes
	state, _ := sm.MakeGenesisState(&genDoc)
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: false,
	})
	if err := stateStore.Save(state); err != nil {
		return 0, 0, 0, err
	}

	for i := 1; i < int(testApp.LastBlockHeight()); i++ {
		state.LastBlockHeight++
		state.LastValidators = state.Validators.Copy()
		if err := stateStore.Save(state); err != nil {
			return 0, 0, 0, err
		}
	}
	pa, _ := state.Validators.GetByIndex(0)
	commit, _, err := makeValidCommit(testApp.ChainID(), 1, comettypes.BlockID{}, state.Validators, privVals)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("error creating valid commit: %w", err)
	}

	blockStore := store.NewBlockStore(dbm.NewMemDB())
	blockExec := sm.NewBlockExecutor(
		stateStore,
		log.NewNopLogger(),
		proxyApp.Consensus(),
		mp,
		evpool,
		blockStore,
	)

	ctx := context.Background()
	createProposalBlockStart := time.Now()
	_, ps, err := blockExec.CreateProposalBlock(ctx, 1, state, commit, pa)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("error creating proposal block: %w", err)
	}
	proposeBlockTime = time.Since(createProposalBlockStart)

	encodeBlockStart := time.Now()
	pps, lastLen, err := comettypes.Encode(ps, comettypes.BlockPartSizeBytes)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("error encoding proposal block: %w", err)
	}
	encodeBlockTime = time.Since(encodeBlockStart)
	cps := propagationtypes.NewCombinedSetFromCompactBlock(&propagationtypes.CompactBlock{
		BpHash:   pps.Hash(),
		Proposal: comettypes.Proposal{BlockID: comettypes.BlockID{PartSetHeader: comettypes.PartSetHeader{Total: ps.Total(), Hash: ps.Hash()}}},
		LastLen:  uint32(lastLen),
	})
	for _, id := range pps.BitArray().GetTrueIndices() {
		part := pps.GetPart(id)
		added, err := cps.AddPart(&propagationtypes.RecoveryPart{
			Height: 11,
			Round:  11,
			Index:  ps.Total() + part.Index,
			Data:   part.Bytes,
			Proof:  &part.Proof,
		}, part.Proof)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("error adding part to proposal block: %w", err)
		}
		if !added {
			return 0, 0, 0, fmt.Errorf("part was not added to proposal block")
		}
	}

	decodeBlockStart := time.Now()
	err = cps.Decode()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("error decoding proposal block: %w", err)
	}
	decodeBlockTime = time.Since(decodeBlockStart)
	return
}

func makeValidCommit(
	chainID string,
	height int64,
	blockID comettypes.BlockID,
	vals *comettypes.ValidatorSet,
	privVals map[string]comettypes.PrivValidator,
) (*comettypes.ExtendedCommit, []*comettypes.Vote, error) {
	sigs := make([]comettypes.ExtendedCommitSig, vals.Size())
	votes := make([]*comettypes.Vote, vals.Size())
	for i := 0; i < vals.Size(); i++ {
		_, val := vals.GetByIndex(int32(i))
		vote, err := comettypes.MakeVote(
			privVals[val.Address.String()],
			chainID,
			int32(i),
			height,
			0,
			cmtproto.PrecommitType,
			blockID,
			time.Now(),
		)
		if err != nil {
			return nil, nil, err
		}
		sigs[i] = vote.ExtendedCommitSig()
		votes[i] = vote
	}
	return &comettypes.ExtendedCommit{
		Height:             height,
		BlockID:            blockID,
		ExtendedSignatures: sigs,
	}, votes, nil
}

// getCPUInfo reads and parses /proc/cpuinfo to extract CPU details
func getCPUInfo() (*CPUInfo, error) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/cpuinfo: %w", err)
	}
	defer file.Close()

	info := &CPUInfo{
		Threads: runtime.NumCPU(),
	}

	scanner := bufio.NewScanner(file)
	processorCount := 0
	coreIDMap := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()

		// Count processors (threads)
		if strings.HasPrefix(line, "processor") {
			processorCount++
		}

		// Count unique physical cores
		if strings.HasPrefix(line, "core id") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				coreID := strings.TrimSpace(parts[1])
				coreIDMap[coreID] = true
			}
		}

		// Get clock speed
		if strings.HasPrefix(line, "cpu MHz") && info.ClockSpeed == "" {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				info.ClockSpeed = strings.TrimSpace(parts[1]) + " MHz"
			}
		}

		// Check for CPU flags (features)
		if strings.HasPrefix(line, "flags") {
			flags := strings.ToLower(line)
			if strings.Contains(flags, "gfni") {
				info.HasGFNI = true
			}
			if strings.Contains(flags, "sha_ni") {
				info.HasSHANI = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading /proc/cpuinfo: %w", err)
	}

	// Set cores count
	if len(coreIDMap) > 0 {
		info.Cores = len(coreIDMap)
	} else {
		// Fallback: assume cores = threads if we can't determine
		info.Cores = info.Threads
	}

	return info, nil
}

// displayCPUInfo prints the CPU specifications
func displayCPUInfo(info *CPUInfo) {
	fmt.Println("=== CPU Specifications ===")
	fmt.Printf("Number of Cores:   %d\n", info.Cores)
	fmt.Printf("Number of Threads: %d\n", info.Threads)
	fmt.Printf("Clock Speed:       %s\n", info.ClockSpeed)
	fmt.Printf("GFNI Support:      %v\n", info.HasGFNI)
	fmt.Printf("SHA-NI Support:    %v\n", info.HasSHANI)
	fmt.Println("==========================")
}

// displayPerformanceResults shows the timing results in a user-friendly format
func displayPerformanceResults(
	prepareProposalTime, processProposalTime, finalizeBlockTime,
	proposeBlockTime, encodeBlockTime, decodeBlockTime time.Duration,
	cpuInfo *CPUInfo,
) {
	fmt.Println("\n=== Performance Test Results ===")

	// Convert to milliseconds for easier reading
	prepareMs := prepareProposalTime.Milliseconds()
	processMs := processProposalTime.Milliseconds()
	finalizeMs := finalizeBlockTime.Milliseconds()
	proposeMs := proposeBlockTime.Milliseconds()
	encodeMs := encodeBlockTime.Milliseconds()
	decodeMs := decodeBlockTime.Milliseconds()

	// Track if any operation is slower than reference
	anySlower := false

	// Helper function to compare and display
	compareAndDisplay := func(name, description string, actualMs int64, referenceMs float64) bool {
		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  What it does: %s\n", description)
		fmt.Printf("  Your time: %d ms\n", actualMs)

		if referenceMs > 0 {
			fmt.Printf("  Reference time: %.0f ms\n", referenceMs)
			if float64(actualMs) > referenceMs {
				fmt.Printf("  Status: SLOWER than reference (%.1fx slower)\n", float64(actualMs)/referenceMs)
				return true
			} else {
				fmt.Printf("  Status: FASTER than reference (%.1fx faster)\n", referenceMs/float64(actualMs))
			}
		} else {
			fmt.Printf("  Status: No reference time set\n")
		}
		return false
	}

	// Display each operation
	anySlower = compareAndDisplay(
		"Prepare Proposal",
		"Getting transactions ready to be included in a block",
		prepareMs,
		referencePrepareProposalMs,
	) || anySlower

	anySlower = compareAndDisplay(
		"Process Proposal",
		"Validating a proposed block before accepting it",
		processMs,
		referenceProcessProposalMs,
	) || anySlower

	anySlower = compareAndDisplay(
		"Finalize Block",
		"Committing the block to the blockchain",
		finalizeMs,
		referenceFinalizeBlockMs,
	) || anySlower

	anySlower = compareAndDisplay(
		"Propose Block",
		"Creating a new block proposal",
		proposeMs,
		referenceProposeBlockMs,
	) || anySlower

	anySlower = compareAndDisplay(
		"Encode Block",
		"Converting the block to transmittable format",
		encodeMs,
		referenceEncodeBlockMs,
	) || anySlower

	anySlower = compareAndDisplay(
		"Decode Block",
		"Converting received block data back to usable format",
		decodeMs,
		referenceDecodeBlockMs,
	) || anySlower

	fmt.Println("\n=== Final Assessment ===")

	// Check if any reference times are set
	hasReferenceTimes := referencePrepareProposalMs > 0 || referenceProcessProposalMs > 0 ||
		referenceFinalizeBlockMs > 0 || referenceProposeBlockMs > 0 ||
		referenceEncodeBlockMs > 0 || referenceDecodeBlockMs > 0

	if !hasReferenceTimes {
		fmt.Println("No reference times have been set yet.")
		fmt.Println("Please update the reference constants in the code with your target values.")
	} else if anySlower {
		fmt.Println("\nWARNING: Your system is slower than the reference requirements!")
		fmt.Println("\nRECOMMENDATION:")
		fmt.Println("To meet the performance requirements, you need to upgrade your hardware to:")
		fmt.Println("  - 32 CPU cores (or more)")
		fmt.Println("  - CPUs that support GFNI (Galois Field New Instructions)")
		fmt.Println("  - CPUs that support SHA-NI (SHA New Instructions)")
		fmt.Println("\nYour current system:")
		fmt.Printf("  - CPU cores: %d\n", cpuInfo.Cores)
		fmt.Printf("  - GFNI support: %v\n", cpuInfo.HasGFNI)
		fmt.Printf("  - SHA-NI support: %v\n", cpuInfo.HasSHANI)
	} else {
		fmt.Println("\nCONGRATULATIONS! Your system meets or exceeds the performance requirements.")
		fmt.Println("Your hardware is capable of running the validator node efficiently.")
	}

	fmt.Println("================================")
}

func generatePayForBlobTransactions(count, size int) (*app.App, [][]byte, error) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 512, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	accountSequence := acc.GetSequence()
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	if err != nil {
		return nil, nil, err
	}

	rawTxs := make([][]byte, 0, count)
	randomBytes := random.Bytes(size)
	blob, err := share.NewBlob(share.RandomNamespace(), randomBytes, 1, acc.GetAddress().Bytes())
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < count; i++ {
		tx, _, err := signer.CreatePayForBlobs(account, []*share.Blob{blob}, user.SetGasLimitAndGasPrice(2549760000, 1), user.SetFee(1_000_000))
		if err != nil {
			return nil, nil, err
		}
		rawTxs = append(rawTxs, tx)
		accountSequence++
		err = signer.SetSequence(account, accountSequence)
		if err != nil {
			return nil, nil, err
		}
	}
	return testApp, rawTxs, nil
}
