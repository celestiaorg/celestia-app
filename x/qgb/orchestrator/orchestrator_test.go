package orchestrator

import (
	"context"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const (
	// nolint
	firstDataCommitmet = "commit"
	// nolint
	firstDCStartHeight = 1
	// nolint
	firstDCEndHeight = 100
)

func TestOrchestratorValsets(t *testing.T) {
	ctx := context.TODO()
	mac := newMockAppClient(t)
	orch := setupTestOrchestrator(t, mac)

	specs := map[string]struct {
		count int
	}{
		"1 valset channel": {count: 1},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			valsets, err := generateValsets(spec.count, crypto.PubkeyToAddress(orch.evmPrivateKey.PublicKey).Hex())
			require.NoError(t, err)

			populateValsetChan(mac.valsets, valsets)
			go func() {
				err := orch.processValsetEvents(ctx, mac.valsets)
				require.NoError(t, err)
			}()
			time.Sleep(2 * time.Second)
			mac.close()

			mac.mutex.Lock()
			defer mac.mutex.Unlock()
			if len(mac.broadCasted) != spec.count {
				t.Error("Not all received valsets got signed")
			}

			broadcastedCopy := make([]sdktypes.Msg, len(mac.broadCasted))
			copy(broadcastedCopy, mac.broadCasted)
			err = verifyOrchestratorValsetSignatures(broadcastedCopy, valsets, orch.bridgeID)
			require.NoError(t, err)
		})
	}
}

func TestOrchestratorDataCommitments(t *testing.T) {
	ctx := context.TODO()
	mac := newMockAppClient(t)
	orch := setupTestOrchestrator(t, mac)

	specs := map[string]struct {
		count int
	}{
		"1 data commitment channel": {count: 1},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			dcs, err := generateDataCommitments(spec.count)
			require.NoError(t, err)

			populateDcChan(mac.commitments, dcs)
			go func() {
				err := orch.processDataCommitmentEvents(ctx, mac.commitments)
				require.NoError(t, err)
			}()
			time.Sleep(2 * time.Second)
			mac.close()

			mac.mutex.Lock()
			defer mac.mutex.Unlock()
			if len(mac.broadCasted) != spec.count {
				t.Error("Not all received data commitments got signed")
			}

			err = verifyOrchestratorDcSignatures(mac.broadCasted, dcs, orch.bridgeID)
			require.NoError(t, err)
		})
	}
}
