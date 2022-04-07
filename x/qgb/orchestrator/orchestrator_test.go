package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"testing"
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
		count  int
		expErr bool
	}{
		"1 valset channel":   {count: 1, expErr: false},
		"10 valset channel":  {count: 10, expErr: false},
		"100 valset channel": {count: 100, expErr: false},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			valsets, err := generateValsets(spec.count)
			require.NoError(t, err)
			populateValsetChan(mac.valsets, valsets)

			err = orch.processValsetEvents(ctx, mac.valsets)
			if spec.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if len(mac.broadCasted) != spec.count {
				t.Error("Not all received valsets got signed")
			}

			err = verifyOrchestratorValsetSignatures(mac.broadCasted, valsets, orch.bridgeID)
			require.NoError(t, err)
		})
	}
}

func verifyOrchestratorValsetSignatures(broadCasted []sdk.Msg, valsets []*types.Valset, bridgeID common.Hash) error {
	for i := 0; i < len(broadCasted); i++ {
		msg := broadCasted[i].(*types.MsgValsetConfirm)
		if msg == nil {
			return errors.New("couldn't cast sdk.Msg to *types.MsgValsetConfirm")
		}
		hash, err := valsets[i].SignBytes(bridgeID)
		sigPublicKeyECDSA, err := crypto.SigToPub(hash.Bytes(), []byte(msg.Signature))
		if err != nil {
			return err
		}
		ethAddress := crypto.PubkeyToAddress(*sigPublicKeyECDSA).Hex()
		if ethAddress != msg.Signature {
			return errors.New("wrong signature for valset")
		}
	}
	return nil
}

func generateValset(nonce int) (*types.Valset, error) {
	validators, err := populateValidators()
	if err != nil {
		return nil, err
	}
	valset, err := types.NewValset(
		uint64(nonce),
		uint64(nonce*10),
		validators,
	)
	if err != nil {
		return nil, err
	}
	return valset, err
}

func generateValsets(count int) ([]*types.Valset, error) {
	valsets := make([]*types.Valset, count)
	for i := 0; i < count; i++ {
		valset, err := generateValset(i)
		if err != nil {
			return nil, err
		}
		valsets[i] = valset
	}
	return valsets, nil
}

func populateValsetChan(valsetChannel chan types.Valset, valsets []*types.Valset) {
	for i := 0; i < len(valsets); i++ {
		valsetChannel <- *valsets[i]
	}
}

func populateValidators() (types.InternalBridgeValidators, error) {
	validators := make(types.InternalBridgeValidators, 5)
	for i := 0; i < 5; i++ {
		validator, err := types.NewInternalBridgeValidator(types.BridgeValidator{
			Power:           10,
			EthereumAddress: fmt.Sprintf("0x9c2B12b5a07FC6D719Ed7646e5041A7E8575832%d", i),
		})
		if err != nil {
			return nil, err
		}
		validators[i] = validator
	}
	return validators, nil
}
