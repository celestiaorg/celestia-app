package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// maxBlocksPerTick bounds how many blocks are fetched per catch-up batch so
// output keeps flowing (and memory stays bounded) when backfilling a large
// range via --start-height.
const maxBlocksPerTick = 256

type blockTrace struct {
	Height           int64   `json:"height"`
	Timestamp        string  `json:"timestamp"`
	BlockTimeSec     float64 `json:"block_time_sec"`
	PFFCount         int     `json:"pff_count"`
	PFBCount         int     `json:"pfb_count"`
	TotalPFFBytes    int64   `json:"total_pff_bytes"`
	TotalPFBBytes    int64   `json:"total_pfb_bytes"`
	PFFThroughputMBs float64 `json:"pff_throughput_mbs"`
	PFBThroughputMBs float64 `json:"pfb_throughput_mbs"`
}

// pffGasTrace records the gas accounting of a single PFF transaction, taken
// from the block results at its tx index.
type pffGasTrace struct {
	Height    int64  `json:"height"`
	TxIndex   int    `json:"tx_index"`
	NumSigs   int    `json:"num_sigs"`
	BlobSize  uint32 `json:"blob_size"`
	Code      uint32 `json:"code"`
	GasWanted int64  `json:"gas_wanted"`
	GasUsed   int64  `json:"gas_used"`
}

// blockResult holds the decoded per-block tallies produced concurrently by a
// worker. Only small scalar fields are retained; the (potentially 32MB+) block
// is decoded and discarded inside fetchAndDecodeBlock.
type blockResult struct {
	height     int64
	blockTime  time.Time
	numTxs     int
	decodeErrs int
	attempts   int
	pffCount   int
	pffBytes   int64
	pfbCount   int
	pfbBytes   int64
	pffGas     []pffGasTrace
	err        error
}

func fibreThroughputCmd() *cobra.Command {
	var (
		rootDir      string
		rpcEndpoints []string
		concurrency  int
		maxRetries   int
		duration     time.Duration
		withTraces   bool
		withGas      bool
		tracesDir    string
		startHeight  int64
	)

	cmd := &cobra.Command{
		Use:   "fibre-throughput",
		Short: "Monitor real-time fibre throughput per block",
		Long:  "Polls blocks from one or more validator RPC endpoints, decodes MsgPayForFibre transactions, and prints throughput per block. Blocks are fetched and decoded concurrently across the configured endpoints (see --concurrency); per-block output is still emitted in height order.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			// Default to every validator's RPC endpoint so downloads are spread
			// across the whole validator set rather than hammering one node.
			if len(rpcEndpoints) == 0 {
				for _, v := range cfg.Validators {
					rpcEndpoints = append(rpcEndpoints, fmt.Sprintf("http://%s:26657", v.PublicIP))
				}
			}
			if concurrency < 1 {
				concurrency = 1
			}

			clients := make([]*http.HTTP, 0, len(rpcEndpoints))
			for _, ep := range rpcEndpoints {
				c, err := http.New(ep, "/websocket")
				if err != nil {
					return fmt.Errorf("failed to create RPC client for %s: %w", ep, err)
				}
				clients = append(clients, c)
			}
			// Pin status polling to a single endpoint so the latest-height
			// reference is consistent across ticks.
			statusClient := clients[0]

			fmt.Printf("RPC endpoints (%d): %v\n", len(clients), rpcEndpoints)
			fmt.Printf("Fetch concurrency: %d\n", concurrency)

			encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			txDecoder := encCfg.TxConfig.TxDecoder()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			go func() {
				<-sigCh
				fmt.Println("\nReceived interrupt, shutting down...")
				cancel()
			}()

			if duration > 0 {
				ctx, cancel = context.WithTimeout(ctx, duration)
				defer cancel()
			}

			var nextHeight int64
			if startHeight > 0 {
				nextHeight = startHeight
			} else {
				statusResp, err := statusClient.Status(ctx)
				if err != nil {
					return fmt.Errorf("failed to get status: %w", err)
				}
				nextHeight = statusResp.SyncInfo.LatestBlockHeight + 1
			}
			fmt.Printf("Starting from height %d\n\n", nextHeight)

			var (
				totalBlocks   int64
				totalPFFBytes int64
				totalPFBBytes int64
				totalSeconds  float64
				prevBlockTime time.Time
			)

			var traceEncoder *json.Encoder
			var traceFile *os.File
			if withTraces {
				if err := os.MkdirAll(tracesDir, 0o755); err != nil {
					return fmt.Errorf("failed to create traces directory: %w", err)
				}
				traceFileName := filepath.Join(tracesDir, fmt.Sprintf("throughput_%s.jsonl", time.Now().Format(time.RFC3339)))
				traceFile, err = os.Create(traceFileName)
				if err != nil {
					return fmt.Errorf("failed to create trace file: %w", err)
				}
				defer traceFile.Close()
				traceEncoder = json.NewEncoder(traceFile)
				fmt.Printf("Writing traces to %s\n", traceFileName)
			}

			var gasEncoder *json.Encoder
			var gasFile *os.File
			if withTraces && withGas {
				gasFileName := filepath.Join(tracesDir, fmt.Sprintf("pff_gas_%s.jsonl", time.Now().Format(time.RFC3339)))
				gasFile, err = os.Create(gasFileName)
				if err != nil {
					return fmt.Errorf("failed to create gas trace file: %w", err)
				}
				defer gasFile.Close()
				gasEncoder = json.NewEncoder(gasFile)
				fmt.Printf("Writing per-PFF gas traces to %s\n", gasFileName)
			}

			// Per-PFF gas records accumulated across the whole run for the
			// end-of-run summary. At the 200 PFF/block cap this is ~20k small
			// structs per 100 blocks, so keeping them all is cheap.
			var allPFFGas []pffGasTrace

			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for ctx.Err() == nil {
				select {
				case <-ctx.Done():
					continue
				case <-ticker.C:
				}

				// Fetch the latest height
				st, err := statusClient.Status(ctx)
				if err != nil {
					if ctx.Err() != nil {
						continue
					}
					fmt.Printf("error fetching status: %v\n", err)
					continue
				}
				latestHeight := st.SyncInfo.LatestBlockHeight

				// Catch up to latestHeight in bounded, concurrently-fetched
				// batches. Each batch is downloaded+decoded in parallel across
				// the endpoints, then processed in strict height order.
				for nextHeight <= latestHeight && ctx.Err() == nil {
					endHeight := latestHeight
					if endHeight-nextHeight+1 > maxBlocksPerTick {
						endHeight = nextHeight + maxBlocksPerTick - 1
					}

					results := fetchBlocksConcurrent(ctx, clients, nextHeight, endHeight, concurrency, maxRetries, txDecoder, withGas)

					for _, res := range results {
						if ctx.Err() != nil {
							break
						}
						if res.err != nil {
							fmt.Printf("error fetching block %d after %d attempt(s): %v\n", res.height, res.attempts, res.err)
							continue
						}

						var blockTimeDelta float64
						if !prevBlockTime.IsZero() {
							blockTimeDelta = res.blockTime.Sub(prevBlockTime).Seconds()
						}
						prevBlockTime = res.blockTime

						var pffThroughputMBs float64
						var pfbThroughputMBs float64
						if blockTimeDelta > 0 {
							pffThroughputMBs = float64(res.pffBytes) / blockTimeDelta / (1024 * 1024)
							pfbThroughputMBs = float64(res.pfbBytes) / blockTimeDelta / (1024 * 1024)
						}

						fmt.Printf("height=%d pff_txs=%d pfb_txs=%d pff_bytes=%dMB pfb_bytes=%dMB block_time=%.2fs pff_throughput=%.2fMB/s pfb_throughput=%.2fMB/s\n",
							res.height, res.pffCount, res.pfbCount, res.pffBytes/(1024*1024), res.pfbBytes/(1024*1024), blockTimeDelta, pffThroughputMBs, pfbThroughputMBs)

						if withGas && len(res.pffGas) > 0 {
							minGas, medianGas, meanGas, maxGas := pffGasStats(res.pffGas)
							fmt.Printf("  pff gas: n=%d min=%d median=%d mean=%d max=%d\n", len(res.pffGas), minGas, medianGas, meanGas, maxGas)
						}

						if res.attempts > 1 {
							fmt.Printf("  note: block %d fetched after %d attempt(s)\n", res.height, res.attempts)
						}

						if res.decodeErrs > 0 {
							fmt.Printf("  warning: %d/%d tx(s) in block %d failed to decode and were skipped\n", res.decodeErrs, res.numTxs, res.height)
						}

						if withGas {
							allPFFGas = append(allPFFGas, res.pffGas...)
							if gasEncoder != nil {
								for _, g := range res.pffGas {
									if err := gasEncoder.Encode(g); err != nil {
										fmt.Printf("error writing gas trace: %v\n", err)
									}
								}
							}
						}

						if traceEncoder != nil {
							trace := blockTrace{
								Height:           res.height,
								Timestamp:        res.blockTime.Format(time.RFC3339),
								BlockTimeSec:     blockTimeDelta,
								PFFCount:         res.pffCount,
								PFBCount:         res.pfbCount,
								TotalPFFBytes:    res.pffBytes,
								TotalPFBBytes:    res.pfbBytes,
								PFFThroughputMBs: pffThroughputMBs,
								PFBThroughputMBs: pfbThroughputMBs,
							}
							if err := traceEncoder.Encode(trace); err != nil {
								fmt.Printf("error writing trace: %v\n", err)
							}
						}

						// Accumulate aggregate totals only over blocks with a
						// measured interval. The first block (and any block
						// following a skipped/failed one) has no usable delta,
						// so counting its bytes would skew the aggregate.
						if blockTimeDelta > 0 {
							totalBlocks++
							totalSeconds += blockTimeDelta
							totalPFFBytes += res.pffBytes
							totalPFBBytes += res.pfbBytes
						}
					}

					nextHeight = endHeight + 1
				}
			}

			// Aggregate throughput is total bytes over total elapsed block time,
			// NOT the mean of per-block rates (which over-weights short,
			// near-empty blocks).
			fmt.Printf("\n--- Summary ---\n")
			fmt.Printf("Blocks measured:    %d\n", totalBlocks)
			fmt.Printf("Block-time elapsed: %.2fs\n", totalSeconds)
			fmt.Printf("Total PFF bytes:    %d\n", totalPFFBytes)
			fmt.Printf("Total PFB bytes:    %d\n", totalPFBBytes)
			if totalSeconds > 0 {
				fmt.Printf("Avg PFF throughput: %.2f MB/s\n", float64(totalPFFBytes)/totalSeconds/(1024*1024))
				fmt.Printf("Avg PFB throughput: %.2f MB/s\n", float64(totalPFBBytes)/totalSeconds/(1024*1024))
			}

			if withGas {
				printPFFGasSummary(allPFFGas)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringSliceVar(&rpcEndpoints, "rpc-endpoint", nil, "CometBFT RPC endpoint(s); repeat or comma-separate for multiple (default: all validators' IP:26657)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 16, "maximum number of blocks to fetch+decode concurrently across endpoints")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 5, "number of times to retry a failed block fetch (rotating across endpoints, with backoff) before skipping it")
	cmd.Flags().DurationVar(&duration, "duration", 0, "how long to run (0 = until Ctrl+C)")
	cmd.Flags().BoolVar(&withTraces, "with-traces", false, "enable JSONL trace file output")
	cmd.Flags().BoolVar(&withGas, "with-gas", false, "record per-PFF gas usage from block results (adds one block_results RPC per block)")
	cmd.Flags().StringVar(&tracesDir, "traces-dir", "./data/monitoring/throughput", "directory for trace files")
	cmd.Flags().Int64Var(&startHeight, "start-height", 0, "block height to start from (0 = latest + 1)")

	return cmd
}

// fetchBlocksConcurrent downloads and decodes blocks [from, to] in parallel,
// bounded by concurrency, distributing requests round-robin across clients.
// Results are returned in ascending height order; a per-block fetch failure
// (after retries) is recorded in that block's result rather than aborting the
// batch.
func fetchBlocksConcurrent(ctx context.Context, clients []*http.HTTP, from, to int64, concurrency, maxRetries int, txDecoder sdk.TxDecoder, withGas bool) []blockResult {
	n := int(to - from + 1)
	results := make([]blockResult, n)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	for i := range n {
		height := from + int64(i)
		g.Go(func() error {
			// startIdx=i preserves the round-robin distribution of first
			// attempts; retries rotate to subsequent endpoints from there.
			results[i] = fetchAndDecodeBlock(gctx, clients, i, height, maxRetries, txDecoder, withGas)
			return nil
		})
	}
	// Workers never return an error (failures are stored per-block), so Wait
	// only surfaces context cancellation, which is already reflected per-block.
	_ = g.Wait()

	return results
}

// fetchAndDecodeBlock fetches a single block and tallies PFF/PFB counts and
// bytes. On a transient RPC failure it retries up to maxRetries times with
// capped exponential backoff, rotating across the available endpoints so a
// single unhealthy node fails over to another. It is safe to call
// concurrently: the tx decoder only reads from the interface registry, which
// is fully populated before any worker starts.
func fetchAndDecodeBlock(ctx context.Context, clients []*http.HTTP, startIdx int, height int64, maxRetries int, txDecoder sdk.TxDecoder, withGas bool) blockResult {
	res := blockResult{height: height}
	h := height

	var lastErr error
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			res.err = ctx.Err()
			res.attempts = attempt
			return res
		}

		// Rotate endpoints across attempts so a flaky node fails over.
		client := clients[(startIdx+attempt)%len(clients)]
		block, err := client.Block(ctx, &h)
		// Per-tx gas lives in block_results, indexed in the same order as
		// block.Txs. Fetch it from the same endpoint so both views agree; a
		// failure retries the whole attempt like a block fetch failure.
		var txsResults []*abci.ExecTxResult
		if err == nil && withGas {
			blockResults, brErr := client.BlockResults(ctx, &h)
			if brErr != nil {
				err = brErr
			} else {
				txsResults = blockResults.TxsResults
			}
		}
		if err == nil {
			res.attempts = attempt + 1
			res.blockTime = block.Block.Time
			res.numTxs = len(block.Block.Txs)

			for txIdx, rawTx := range block.Block.Txs {
				sdkTx, decErr := txDecoder(rawTx)
				if decErr != nil {
					res.decodeErrs++
					continue
				}
				for _, msg := range sdkTx.GetMsgs() {
					if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok {
						res.pffCount++
						res.pffBytes += int64(pff.PaymentPromise.BlobSize)
						if txIdx < len(txsResults) {
							txRes := txsResults[txIdx]
							res.pffGas = append(res.pffGas, pffGasTrace{
								Height:    height,
								TxIndex:   txIdx,
								NumSigs:   len(pff.ValidatorSignatures),
								BlobSize:  pff.PaymentPromise.BlobSize,
								Code:      txRes.Code,
								GasWanted: txRes.GasWanted,
								GasUsed:   txRes.GasUsed,
							})
						}
						continue
					}
					if pfb, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
						res.pfbCount++
						for _, size := range pfb.BlobSizes {
							res.pfbBytes += int64(size)
						}
					}
				}
			}
			return res
		}

		lastErr = err
		if attempt >= maxRetries {
			res.err = lastErr
			res.attempts = attempt + 1
			return res
		}
		// Back off before the next attempt, aborting promptly on cancel.
		if !sleepCtx(ctx, backoffDuration(attempt)) {
			res.err = ctx.Err()
			res.attempts = attempt + 1
			return res
		}
	}
}

// pffGasStats returns min/median/mean/max of gas_used over the given records.
// Callers must pass a non-empty slice.
func pffGasStats(records []pffGasTrace) (minGas, medianGas, meanGas, maxGas int64) {
	gas := make([]int64, len(records))
	var sum int64
	for i, r := range records {
		gas[i] = r.GasUsed
		sum += r.GasUsed
	}
	sort.Slice(gas, func(i, j int) bool { return gas[i] < gas[j] })
	return gas[0], gas[len(gas)/2], sum / int64(len(gas)), gas[len(gas)-1]
}

// printPFFGasSummary prints run-wide gas_used stats grouped by validator
// signature count. Failed txs (code != 0) abort execution early, so their gas
// doesn't reflect the full PFF cost; they are counted but excluded from stats.
func printPFFGasSummary(all []pffGasTrace) {
	fmt.Printf("\n--- PFF gas summary ---\n")
	if len(all) == 0 {
		fmt.Println("No PFF gas records collected.")
		return
	}

	var ok []pffGasTrace
	var failed int
	for _, g := range all {
		if g.Code == abci.CodeTypeOK {
			ok = append(ok, g)
		} else {
			failed++
		}
	}
	fmt.Printf("PFFs recorded: %d (%d failed, excluded from stats)\n", len(all), failed)
	if len(ok) == 0 {
		return
	}

	bySigs := make(map[int][]pffGasTrace)
	for _, g := range ok {
		bySigs[g.NumSigs] = append(bySigs[g.NumSigs], g)
	}
	sigCounts := make([]int, 0, len(bySigs))
	for n := range bySigs {
		sigCounts = append(sigCounts, n)
	}
	sort.Ints(sigCounts)

	for _, n := range sigCounts {
		minGas, medianGas, meanGas, maxGas := pffGasStats(bySigs[n])
		fmt.Printf("sigs=%-3d n=%-6d min=%-8d median=%-8d mean=%-8d max=%-8d\n",
			n, len(bySigs[n]), minGas, medianGas, meanGas, maxGas)
	}
	minGas, medianGas, meanGas, maxGas := pffGasStats(ok)
	fmt.Printf("overall  n=%-6d min=%-8d median=%-8d mean=%-8d max=%-8d\n",
		len(ok), minGas, medianGas, meanGas, maxGas)
}

// sleepCtx sleeps for d, or returns false early if ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// backoffDuration returns a capped exponential backoff for the given 0-based
// retry attempt: 250ms, 500ms, 1s, 2s, 4s, ... capped at 5s.
func backoffDuration(attempt int) time.Duration {
	if attempt > 5 {
		attempt = 5
	}
	d := 250 * time.Millisecond * time.Duration(int64(1)<<uint(attempt))
	return min(d, 5*time.Second)
}
