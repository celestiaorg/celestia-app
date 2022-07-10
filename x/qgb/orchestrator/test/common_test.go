package test

import (
	"context"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"
	"sync"
	"testing"
)

// TODO update the tests to reflect the new design.
func setupTestOrchestrator(t *testing.T, bc orchestrator.BroadcasterI) *orchestrator.Orchestrator {
	priv, err := crypto.HexToECDSA(testPriv)
	if err != nil {
		panic(err)
	}
	mockQuerier := NewMockQuerier(
		nil,
		0)
	mockRetier := NewMockRetrier()

	return orchestrator.NewOrchestrator(
		tmlog.NewTMLogger(os.Stdout),
		mockQuerier,
		bc,
		mockRetier,
		testutil.GenerateKeyringSigner(t, testutil.TestAccName),
		*priv,
	)
}

// nolint
type mockEVMClient struct {
	vasletUpdates      []valsetUpdate
	dataRootTupleRoots []dataRootTupleRoot
	mtx                *sync.Mutex
}

type (
	// nolint
	valsetUpdate struct {
		nonce, threshHold uint64
		valset            types.Valset
		sigs              []wrapper.Signature
	}
	// nolint
	dataRootTupleRoot struct {
		tupleRoot               common.Hash
		lastDataCommitmentNonce uint64
		currentValset           types.Valset
		sigs                    []wrapper.Signature
	}
)

// nolint
func (mec *mockEVMClient) UpdateValidatorSet(ctx context.Context, nonce, threshHold uint64, valset types.Valset, sigs []wrapper.Signature) error {
	mec.mtx.Lock()
	defer mec.mtx.Unlock()
	mec.vasletUpdates = append(
		mec.vasletUpdates,
		valsetUpdate{
			nonce:      nonce,
			threshHold: threshHold,
			valset:     valset,
			sigs:       sigs,
		},
	)
	return nil
}

// nolint
func (mec *mockEVMClient) SubmitDataRootTupleRoot(ctx context.Context, tupleRoot common.Hash, lastDataCommitmentNonce uint64, currentValset types.Valset, sigs []wrapper.Signature) error {
	mec.mtx.Lock()
	defer mec.mtx.Unlock()
	mec.dataRootTupleRoots = append(
		mec.dataRootTupleRoots,
		dataRootTupleRoot{
			tupleRoot:               tupleRoot,
			lastDataCommitmentNonce: lastDataCommitmentNonce,
			currentValset:           currentValset,
			sigs:                    sigs,
		},
	)
	return nil
}

// nolint
func (mec *mockEVMClient) StateLastDataRootTupleRootNonce(opts *bind.CallOpts) (uint64, error) {
	return uint64(len(mec.dataRootTupleRoots)), nil
}
