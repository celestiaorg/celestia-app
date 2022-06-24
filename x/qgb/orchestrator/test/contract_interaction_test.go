package test

import (
	"context"
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/keeper/keystore"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"
)

const (
	testAddr  = "9c2B12b5a07FC6D719Ed7646e5041A7E85758329"
	testPriv  = "64a1d6f0e760a8d62b4afdde4096f16f51b401eaaecc915740f71770ea76a8ad"
	testAddr2 = "e650B084f05C6194f6e552e3b9f08718Bc8a9d56"
)

var (
	bID           = ethcmn.HexToHash(types.ValidatorSetDomainSeparator)
	initialValSet types.Valset
)

type QGBTestSuite struct {
	suite.Suite
	auth    *bind.TransactOpts
	gAlloc  core.GenesisAlloc
	sim     *backends.SimulatedBackend
	wrapper *wrapper.QuantumGravityBridge
	pSigner keystore.PersonalSignFn
}

func TestRunQGBSuite(t *testing.T) {
	suite.Run(t, new(QGBTestSuite))
}

func (s *QGBTestSuite) SetupTest() {
	key, err := crypto.HexToECDSA(testPriv)
	s.Require().NoError(err)

	//nolint
	s.auth = bind.NewKeyedTransactor(key)
	s.auth.GasLimit = 10000000000000
	s.auth.GasPrice = big.NewInt(8750000000)
	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(key)
	s.NoError(err)
	s.pSigner = personalSignFn

	valSet := types.Valset{
		Nonce:  0,
		Height: 1,
		Members: []types.BridgeValidator{
			{
				Power:           5000,
				EthereumAddress: testAddr,
			},
		},
	}

	initialValSet = valSet

	vsHash, err := valSet.Hash()
	s.NoError(err)

	genBal := &big.Int{}
	genBal.SetString("999999999999999999999999999999999999999999", 20)

	s.gAlloc = map[ethcmn.Address]core.GenesisAccount{
		s.auth.From: {Balance: genBal},
	}

	s.sim = backends.NewSimulatedBackend(s.gAlloc, 100000000000000)

	_, _, qgbWrapper, err := wrapper.DeployQuantumGravityBridge(
		s.auth,
		s.sim,
		bID,
		big.NewInt(int64(initialValSet.Nonce)),
		big.NewInt(int64(initialValSet.TwoThirdsThreshold())),
		vsHash,
	)
	s.NoError(err)
	s.wrapper = qgbWrapper

	s.sim.Commit()

	cbid, err := qgbWrapper.BRIDGEID(nil)
	s.NoError(err)
	s.Require().Equal(bID.Hex(), "0x"+ethcmn.Bytes2Hex(cbid[:]))
}

func (s *QGBTestSuite) TestSubmitDataCommitment() {
	// we just need something to sign over, it doesn't matter what
	commitment := ethcmn.HexToHash(types.ValidatorSetDomainSeparator)
	signBytes := types.DataCommitmentTupleRootSignBytes(
		bID,
		big.NewInt(1),
		commitment[:],
	)

	signature, err := s.pSigner(s.auth.From, signBytes.Bytes())
	s.NoError(err)

	ethVals := make([]wrapper.Validator, len(initialValSet.Members))
	for i, val := range initialValSet.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EthereumAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	hexSig := ethcmn.Bytes2Hex(signature)
	v, r, ss := orchestrator.SigToVRS(hexSig)
	tx, err := s.wrapper.SubmitDataRootTupleRoot(
		s.auth,
		big.NewInt(1),
		big.NewInt(0), // TODO get this from the setup
		commitment,
		ethVals,
		[]wrapper.Signature{
			{
				V: v,
				R: r,
				S: ss,
			},
		},
	)
	s.NoError(err)
	s.sim.Commit()

	recp, err := s.sim.TransactionReceipt(context.TODO(), tx.Hash())
	s.NoError(err)
	s.Assert().Equal(uint64(1), recp.Status)

	dcNonce, err := s.wrapper.StateEventNonce(nil)
	s.NoError(err)
	s.Assert().Equal(0, dcNonce.Cmp(big.NewInt(1)))
}

func (s *QGBTestSuite) TestUpdateValset() {
	updatedValset := types.Valset{
		Members: []types.BridgeValidator{
			{
				EthereumAddress: testAddr,
				Power:           5000,
			},
			{
				EthereumAddress: testAddr2,
				Power:           5000,
			},
		},
		Nonce:  1,
		Height: 2,
	}

	newVsHash, err := updatedValset.Hash()
	s.NoError(err)
	signBytes, err := updatedValset.SignBytes(bID)
	s.NoError(err)
	signature, err := s.pSigner(s.auth.From, signBytes.Bytes())
	s.NoError(err)

	hexSig := ethcmn.Bytes2Hex(signature)

	ethVals := make([]wrapper.Validator, len(initialValSet.Members))
	for i, val := range initialValSet.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EthereumAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	thresh := updatedValset.TwoThirdsThreshold()

	err = s.updateNonce()
	s.Require().NoError(err)

	v, r, ss := orchestrator.SigToVRS(hexSig)

	tx, err := s.wrapper.UpdateValidatorSet(
		s.auth,
		big.NewInt(1),
		big.NewInt(0),
		big.NewInt(int64(thresh)),
		newVsHash,
		ethVals,
		[]wrapper.Signature{
			{
				V: v,
				R: r,
				S: ss,
			},
		},
	)
	s.NoError(err)
	s.sim.Commit()

	recp, err := s.sim.TransactionReceipt(context.TODO(), tx.Hash())
	s.NoError(err)
	s.Equal(uint64(1), recp.Status)

	valSetThresh, err := s.wrapper.StatePowerThreshold(nil)
	s.NoError(err)
	// check that the validator set was changed.
	s.Equal(0, valSetThresh.Cmp(big.NewInt(6668)))
}

func (s *QGBTestSuite) updateNonce() error {
	nonce, err := s.sim.NonceAt(context.TODO(), s.auth.From, nil)
	if err != nil {
		return err
	}
	s.auth.Nonce = big.NewInt(int64(nonce))
	return nil
}
