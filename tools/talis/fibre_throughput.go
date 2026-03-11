package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/spf13/cobra"
)

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

func fibreThroughputCmd() *cobra.Command {
	var (
		rootDir     string
		rpcEndpoint string
		duration    time.Duration
		withTraces  bool
		tracesDir   string
		startHeight int64
	)

	cmd := &cobra.Command{
		Use:   "fibre-throughput",
		Short: "Monitor real-time fibre throughput per block",
		Long:  "Polls blocks from a validator's RPC endpoint, decodes MsgPayForFibre transactions, and prints throughput per block.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			if len(cfg.Validators) == 0 {
				return fmt.Errorf("no validators found in config")
			}

			if rpcEndpoint == "" {
				rpcEndpoint = fmt.Sprintf("http://%s:26657", cfg.Validators[0].PublicIP)
			}

			fmt.Printf("RPC endpoint: %s\n", rpcEndpoint)

			client, err := http.New(rpcEndpoint, "/websocket")
			if err != nil {
				return fmt.Errorf("failed to create RPC client: %w", err)
			}

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
				statusResp, err := client.Status(ctx)
				if err != nil {
					return fmt.Errorf("failed to get status: %w", err)
				}
				nextHeight = statusResp.SyncInfo.LatestBlockHeight + 1
			}
			fmt.Printf("Starting from height %d\n\n", nextHeight)

			var (
				totalBlocks     int64
				totalBytes      int64
				prevBlockTime   time.Time
				totalThroughput float64
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

			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()

			for ctx.Err() == nil {
				select {
				case <-ctx.Done():
					continue
				case <-ticker.C:
				}

				// Fetch the latest height
				st, err := client.Status(ctx)
				if err != nil {
					if ctx.Err() != nil {
						continue
					}
					fmt.Printf("error fetching status: %v\n", err)
					continue
				}
				latestHeight := st.SyncInfo.LatestBlockHeight

				// Process all new blocks
				for h := nextHeight; h <= latestHeight && ctx.Err() == nil; h++ {
					height := h
					block, err := client.Block(ctx, &height)
					if err != nil {
						if ctx.Err() != nil {
							break
						}
						fmt.Printf("error fetching block %d: %v\n", h, err)
						continue
					}

					blockTime := block.Block.Time
					var blockTimeDelta float64
					if !prevBlockTime.IsZero() {
						blockTimeDelta = blockTime.Sub(prevBlockTime).Seconds()
					}
					prevBlockTime = blockTime

					var pffCount int
					var pffBytes int64
					var pfbCount int
					var pfbBytes int64
					for _, rawTx := range block.Block.Txs {
						sdkTx, err := txDecoder(rawTx)
						if err != nil {
							continue
						}
						for _, msg := range sdkTx.GetMsgs() {
							if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok {
								pffCount++
								pffBytes += int64(pff.PaymentPromise.BlobSize)
								continue
							}
							if pfb, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
								pfbCount++
								for _, size := range pfb.BlobSizes {
									pfbBytes += int64(size)
								}
							}
						}
					}

					var pffThroughputMBs float64
					var pfbThroughputMBs float64
					if blockTimeDelta > 0 {
						pffThroughputMBs = float64(pffBytes) / blockTimeDelta / (1024 * 1024)
						pfbThroughputMBs = float64(pfbBytes) / blockTimeDelta / (1024 * 1024)
					}

					fmt.Printf("height=%d pff_txs=%d pfb_txs=%d pff_bytes=%dMB pfb_bytes=%dMB block_time=%.2fs pff_throughput=%.2fMB/s pfb_throughput=%.2fMB/s\n",
						h, pffCount, pfbCount, pffBytes/(1024*1024), pfbBytes/(1024*1024), blockTimeDelta, pffThroughputMBs, pfbThroughputMBs)

					if traceEncoder != nil {
						trace := blockTrace{
							Height:           h,
							Timestamp:        blockTime.Format(time.RFC3339),
							BlockTimeSec:     blockTimeDelta,
							PFFCount:         pffCount,
							PFBCount:         pfbCount,
							TotalPFFBytes:    pffBytes,
							TotalPFBBytes:    pfbBytes,
							PFFThroughputMBs: pffThroughputMBs,
							PFBThroughputMBs: pfbThroughputMBs,
						}
						if err := traceEncoder.Encode(trace); err != nil {
							fmt.Printf("error writing trace: %v\n", err)
						}
					}

					totalBytes += pffBytes
					if blockTimeDelta > 0 {
						totalBlocks++
						totalThroughput += pffThroughputMBs
					}

					nextHeight = h + 1
				}
			}

			fmt.Printf("\n--- Summary ---\n")
			fmt.Printf("Total blocks:  %d\n", totalBlocks)
			fmt.Printf("Total bytes:   %d\n", totalBytes)
			if totalBlocks > 0 {
				fmt.Printf("Avg throughput: %.2f MB/s\n", totalThroughput/float64(totalBlocks))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().StringVar(&rpcEndpoint, "rpc-endpoint", "", "CometBFT RPC endpoint (default: first validator IP:26657)")
	cmd.Flags().DurationVar(&duration, "duration", 0, "how long to run (0 = until Ctrl+C)")
	cmd.Flags().BoolVar(&withTraces, "with-traces", false, "enable JSONL trace file output")
	cmd.Flags().StringVar(&tracesDir, "traces-dir", "./data/monitoring/throughput", "directory for trace files")
	cmd.Flags().Int64Var(&startHeight, "start-height", 0, "block height to start from (0 = latest + 1)")

	return cmd
}
