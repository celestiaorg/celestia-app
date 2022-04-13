package orchestrator

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/testutil"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

func setupTestOrchestrator(t *testing.T, ac AppClient) *orchestrator {
	priv, err := crypto.HexToECDSA(testPriv)
	if err != nil {
		panic(err)
	}
	return &orchestrator{
		appClient:           ac,
		logger:              tmlog.NewTMLogger(tmlog.NewSyncWriter(os.Stderr)),
		orchestratorAddress: crypto.PubkeyToAddress(priv.PublicKey).Hex(),
		bridgeID:            ethcmn.BytesToHash([]byte("test bridge")),
		evmPrivateKey:       *priv,
	}
}

var _ AppClient = &mockAppClient{}

type mockAppClient struct {
	valsets     chan types.Valset
	commitments chan ExtendedDataCommitment

	signer *paytypes.KeyringSigner

	broadCasted []sdk.Msg
	dcConfirms  map[string][]types.MsgDataCommitmentConfirm
	vsConfirms  map[uint64][]types.MsgValsetConfirm
	lastValset  types.Valset
	mutex       *sync.Mutex
}

func newMockAppClient(t *testing.T) *mockAppClient {
	return &mockAppClient{
		valsets:     make(chan types.Valset, 10),
		commitments: make(chan ExtendedDataCommitment, 10),
		dcConfirms:  make(map[string][]types.MsgDataCommitmentConfirm),
		vsConfirms:  make(map[uint64][]types.MsgValsetConfirm),
		signer:      testutil.GenerateKeyringSigner(t, testutil.TestAccName),
		mutex:       &sync.Mutex{},
	}
}

func (mac *mockAppClient) close() {
	close(mac.commitments)
	close(mac.valsets)
}

// nolint
func (mac *mockAppClient) pushValidatorSet(valset types.Valset) {
	mac.valsets <- valset
}

// TODO fix all of the `nolint` flags
// nolint
func (mac *mockAppClient) pushDataCommitment(commit ExtendedDataCommitment) {
	mac.commitments <- commit
}

// nolint
func (mac *mockAppClient) setDataCommitmentConfirms(commit string, confirms []types.MsgDataCommitmentConfirm) {
	mac.dcConfirms[commit] = confirms
}

// nolint
func (mac *mockAppClient) setValsetConfirms(nonce uint64, confirms []types.MsgValsetConfirm) {
	mac.vsConfirms[nonce] = confirms
}

// nolint
func (mac *mockAppClient) setLatestValset(valset types.Valset) {
	mac.lastValset = valset
}

func (mac *mockAppClient) SubscribeValset(ctx context.Context) (<-chan types.Valset, error) {
	return mac.valsets, nil
}

func (mac *mockAppClient) SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error) {
	return mac.commitments, nil
}

func (mac *mockAppClient) BroadcastTx(ctx context.Context, msg sdk.Msg) error {
	mac.mutex.Lock()
	defer mac.mutex.Unlock()
	mac.broadCasted = append(mac.broadCasted, msg)
	return nil
}

func (mac *mockAppClient) QueryDataCommitments(ctx context.Context, commit string) ([]types.MsgDataCommitmentConfirm, error) {
	return mac.dcConfirms[commit], nil
}

func (mac *mockAppClient) QueryTwoThirdsDataCommitmentConfirms(ctx context.Context, timeout time.Duration, commit string) ([]types.MsgDataCommitmentConfirm, error) {
	return mac.dcConfirms[commit], nil
}
func (mac *mockAppClient) QueryTwoThirdsValsetConfirms(ctx context.Context, timeout time.Duration, valset types.Valset) ([]types.MsgValsetConfirm, error) {
	return mac.vsConfirms[valset.Nonce], nil
}

func (mac *mockAppClient) OrchestratorAddress() sdk.AccAddress {
	return mac.signer.GetSignerInfo().GetAddress()
}

func (mac *mockAppClient) QueryLastValset(ctx context.Context) (types.Valset, error) {
	return mac.lastValset, nil
}

func (mac *mockAppClient) QueryLastValsets(ctx context.Context) ([]types.Valset, error) {
	// TODO update
	return nil, nil
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
