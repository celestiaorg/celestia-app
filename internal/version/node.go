package version

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/log"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/rpc/core"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/spf13/viper"
)

func readConfig(v *viper.Viper) (config, error) {
	cfg := config{endpoint: v.GetString("version-reporting.endpoint"), interval: defaultInterval}
	if cfg.endpoint == "" {
		return cfg, nil
	}
	if v.IsSet("version-reporting.interval") {
		var err error
		cfg.interval, err = time.ParseDuration(v.GetString("version-reporting.interval"))
		if err != nil {
			return cfg, fmt.Errorf("invalid version-reporting interval: %w", err)
		}
	}
	return cfg, cfg.validate()
}

// Run reports until ctx is canceled. Call it in a node lifecycle worker after
// startup. Errors affect reporting only, never the node's error group.
// A nil node covers gRPC-only and external-ABCI processes, which must not report.
func Run(ctx context.Context, n *node.Node, v *viper.Viper, logger log.Logger) {
	if n == nil || ctx.Err() != nil {
		return
	}
	cfg, err := readConfig(v)
	if err != nil {
		logger.Warn("version reporting disabled", "err", err)
		return
	}
	if cfg.endpoint == "" {
		return
	}
	// ConfigureRPC uses the node's actual signer, including a remote signer.
	// Its Status method runs locally, without an HTTP server or staking queries.
	env, err := configureRPC(ctx, n.ConfigureRPC, cfg.interval, logger)
	if err != nil {
		return
	}
	status := func() (*ctypes.ResultStatus, error) {
		return env.Status(&rpctypes.Context{})
	}
	if err := run(ctx, cfg, n.GenesisDoc().ChainID, status, logger); err != nil {
		logger.Warn("version reporting stopped", "err", err)
	}
}

// ConfigureRPC can wait for the remote signer without accepting a context. Allow
// the lifecycle worker to exit on cancellation so node cleanup can close the
// signer connection. One resolver retries completed failures at the reporting
// interval; calls never overlap. Core bounds its signer attempts, and the buffered
// result lets an outstanding call finish after the caller has stopped waiting.
func configureRPC(ctx context.Context, configure func() (*core.Environment, error), interval time.Duration, logger log.Logger) (*core.Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ready := make(chan *core.Environment, 1)
	go func() {
		for ctx.Err() == nil {
			env, err := configure()
			if err == nil {
				ready <- env
				return
			}
			if ctx.Err() != nil {
				return
			}
			logger.Warn("cannot initialize version reporting; will retry next interval", "err", err)
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case env := <-ready:
		return env, nil
	}
}
