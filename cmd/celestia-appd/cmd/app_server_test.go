package cmd

import (
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestNewAppServerAppliesTimeoutFlags(t *testing.T) {
	opts := simtestutil.AppOptionsMap{
		flags.FlagHome:              t.TempDir(),
		flags.FlagChainID:           "test",
		server.FlagPruning:          "nothing",
		TimeoutCommitFlag:           5 * time.Millisecond,
		DelayedPrecommitTimeoutFlag: 7 * time.Millisecond,
		DynamicTimeoutCommitFlag:    3 * time.Millisecond,
	}
	celestiaApp, ok := NewAppServer(log.NewNopLogger(), dbm.NewMemDB(), nil, opts).(*app.App)
	require.True(t, ok)

	got := celestiaApp.TimeoutInfo(11)
	require.Equal(t, 5*time.Millisecond, got.TimeoutCommit)
	require.Equal(t, 7*time.Millisecond, got.DelayedPrecommitTimeout)
	require.Equal(t, 3*time.Millisecond, got.DynamicTimeoutCommit)
}

// TestDisableOptimisticExecutionFlag runs a node built by NewAppServer and
// checks that the flag stops ProcessProposal from starting speculative runs.
func TestDisableOptimisticExecutionFlag(t *testing.T) {
	t.Parallel()
	start := &cobra.Command{}
	addStartFlags(start)
	require.NotNil(t, start.Flags().Lookup(FlagDisableOptimisticExecution))

	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable=%v", disable), func(t *testing.T) {
			t.Parallel()
			logs := &recordingLogger{msgs: &recorded{}}
			cfg := newRollbackHome(t)
			cfg.AppOptions.Set(FlagDisableOptimisticExecution, disable)
			creator := func(_ log.Logger, db dbm.DB, w io.Writer, opts servertypes.AppOptions) servertypes.Application {
				return NewAppServer(logs, db, w, opts)
			}
			runNode(t, cfg, creator, 4)

			require.True(t, logs.msgs.has(fmt.Sprintf("optimistic execution [enabled %v]", !disable)))
			require.Equal(t, !disable, logs.msgs.has("OE started"), "speculative runs")
		})
	}
}

type recorded struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorded) add(msg string, kv []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg, fmt.Sprintf("%s %v", msg, kv))
}

func (r *recorded) has(msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.msgs, msg)
}

// recordingLogger records the message of every log call at any level.
type recordingLogger struct{ msgs *recorded }

func (l *recordingLogger) Info(msg string, kv ...any)  { l.msgs.add(msg, kv) }
func (l *recordingLogger) Error(msg string, kv ...any) { l.msgs.add(msg, kv) }
func (l *recordingLogger) Debug(msg string, kv ...any) { l.msgs.add(msg, kv) }
func (l *recordingLogger) Trace(msg string, kv ...any) { l.msgs.add(msg, kv) }
func (l *recordingLogger) Warn(msg string, kv ...any)  { l.msgs.add(msg, kv) }
func (l *recordingLogger) With(...any) log.Logger      { return l }
func (l *recordingLogger) Impl() any                   { return l }
