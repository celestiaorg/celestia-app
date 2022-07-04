package test

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestOrchestratorProcessValset(t *testing.T) {
	ctx := context.TODO()
	mb := NewMockBroadcaster()
	orch := setupTestOrchestrator(t, mb)

	specs := map[string]struct {
		count int
	}{
		"1 valset": {count: 1},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			valset, err := generateValset(spec.count, crypto.PubkeyToAddress(orch.EvmPrivateKey.PublicKey).Hex())
			require.NoError(t, err)

			err = orch.ProcessValsetEvent(ctx, *valset)
			require.NoError(t, err)

			if len(mb.broadcasted) != spec.count {
				t.Error("valset was not broadcasted")
			}

			err = verifyOrchestratorValsetSignature(mb.broadcasted[0], valset)
			require.NoError(t, err)
		})
	}
}

func TestOrchestratorDataCommitments(t *testing.T) {
	ctx := context.TODO()
	mb := NewMockBroadcaster()
	orch := setupTestOrchestrator(t, mb)

	specs := map[string]struct {
		count int
	}{
		"1 data commitment": {count: 1},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			dc, err := generateDc(spec.count)
			require.NoError(t, err)

			err = orch.ProcessDataCommitmentEvent(ctx, dc)
			require.NoError(t, err)

			if len(mb.broadcasted) != spec.count {
				t.Error("data commitment was not broadcasted")
			}

			err = verifyOrchestratorDcSignature(mb.broadcasted[0], dc)
			require.NoError(t, err)
		})
	}
}
