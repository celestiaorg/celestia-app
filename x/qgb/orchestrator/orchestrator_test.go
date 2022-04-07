package orchestrator

import (
	"context"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const (
	firstDataCommitmet = "commit"
	firstDCStartHeight = 1
	firstDCEndHeight   = 100
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

			if len(mac.broadCasted) != spec.count {
				t.Error("Not all received valsets got signed")
			}

			err = verifyOrchestratorValsetSignatures(mac.broadCasted, valsets, orch.bridgeID)
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

			if len(mac.broadCasted) != spec.count {
				t.Error("Not all received data commitments got signed")
			}

			err = verifyOrchestratorDcSignatures(mac.broadCasted, dcs, orch.bridgeID)
			require.NoError(t, err)
		})
	}
}
