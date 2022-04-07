package orchestrator

import (
	"context"
	"errors"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"strings"
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
			go orch.processValsetEvents(ctx, mac.valsets)
			time.Sleep(2 * time.Second)

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
		sigPublicKeyECDSA, err := crypto.SigToPub(hash.Bytes(), common.Hex2Bytes(msg.Signature))
		if err != nil {
			return err
		}
		ethAddress := crypto.PubkeyToAddress(*sigPublicKeyECDSA).Hex()
		if strings.Compare(ethAddress, msg.Signature) == 0 {
			return errors.New("wrong signature for valset")
		}
	}
	return nil
}

func generateValset(nonce int, ethAddress string) (*types.Valset, error) {
	validators, err := populateValidators(ethAddress)
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

func generateValsets(count int, ethAddress string) ([]*types.Valset, error) {
	valsets := make([]*types.Valset, count)
	for i := 0; i < count; i++ {
		valset, err := generateValset(i, ethAddress)
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

func populateValidators(ethAddress string) (types.InternalBridgeValidators, error) {
	validators := make(types.InternalBridgeValidators, 1)
	validator, err := types.NewInternalBridgeValidator(
		types.BridgeValidator{
			Power:           80,
			EthereumAddress: ethAddress,
		})
	if err != nil {
		return nil, err
	}
	validators[0] = validator
	return validators, nil
}
