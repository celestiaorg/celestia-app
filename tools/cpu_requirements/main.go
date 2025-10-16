package main

import (
	"bufio"
	"fmt"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cometbft/cometbft/abci/types"
	"os"
	"runtime"
	"strings"
	"time"
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
		os.Exit(1)
	}

	info, err := getCPUInfo()
	if err != nil {
		fmt.Printf("Error getting CPU info: %v\n", err)
		os.Exit(1)
	}

	displayCPUInfo(info)

	app, txs, err := generatePayForBlobTransactions(140, 1024*1024)
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

	fmt.Println("Done")
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
	fmt.Printf("PrepareProposal took %vms\n", elapsed.Milliseconds())
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
	fmt.Printf("ProcessProposal took %vms\n", elapsed.Milliseconds())
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
	fmt.Printf("FinalizeBlock took %vms\n", elapsed.Milliseconds())
	return elapsed, nil
}

func runProposeBlock(testApp *app.App, txs [][]byte) (time.Duration, error) {
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
	fmt.Printf("FinalizeBlock took %vms\n", elapsed.Milliseconds())
	return elapsed, nil
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
