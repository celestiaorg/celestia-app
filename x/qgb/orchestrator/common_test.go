package orchestrator

import (
	"context"
	"github.com/celestiaorg/celestia-app/testutil"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"
	"testing"
	"time"
)

func setupTestOrchestrator(t *testing.T, ac AppClient) *orchestrator {
	priv, err := crypto.HexToECDSA(testPriv)
	if err != nil {
		panic(err)
	}
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
}

func newMockAppClient(t *testing.T) *mockAppClient {
	return &mockAppClient{
		valsets:     make(chan types.Valset, 10),
		commitments: make(chan ExtendedDataCommitment, 10),
		dcConfirms:  make(map[string][]types.MsgDataCommitmentConfirm),
		vsConfirms:  make(map[uint64][]types.MsgValsetConfirm),
		signer:      testutil.GenerateKeyringSigner(t, testutil.TestAccName),
	}
}

func (mac *mockAppClient) close() {
	close(mac.commitments)
	close(mac.valsets)
}

func (mac *mockAppClient) pushValidatorSet(valset types.Valset) {
	mac.valsets <- valset
}

func (mac *mockAppClient) pushDataCommitment(commit ExtendedDataCommitment) {
	mac.commitments <- commit
}

func (mac *mockAppClient) setDataCommitmentConfirms(commit string, confirms []types.MsgDataCommitmentConfirm) {
	mac.dcConfirms[commit] = confirms
}

func (mac *mockAppClient) setValsetConfirms(nonce uint64, confirms []types.MsgValsetConfirm) {
	mac.vsConfirms[nonce] = confirms
}

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
