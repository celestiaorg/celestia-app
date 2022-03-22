package test

import (
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/keystore"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/suite"
)

var (
	bID           = ethcmn.HexToHash("0xqwerty")
	initialValSet types.Valset
)

type QGBTestSuite struct {
	suite.Suite
	auth    *bind.TransactOpts
	address ethcmn.Address
	gAlloc  core.GenesisAlloc
	sim     *backends.SimulatedBackend
	wrapper *wrapper.QuantumGravityBridge
	pSigner keystore.PersonalSignFn
}

func TestRunQGBSuite(t *testing.T) {
	suite.Run(t, new(QGBTestSuite))
}

func (s *QGBTestSuite) SetupTest() {
	key, _ := crypto.GenerateKey()
	s.auth = bind.NewKeyedTransactor(key)
	s.auth.GasLimit = 1000000000000000
	s.auth.GasPrice = big.NewInt(875000000)
	s.address = s.auth.From
	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(key)
	s.NoError(err)
	s.pSigner = personalSignFn

	valSet := types.Valset{
		Nonce:  0,
		Height: 1,
		Members: []types.BridgeValidator{
			{
				Power:           5000,
				EthereumAddress: s.auth.From.Hex(),
			},
		},
	}

	initialValSet = valSet

	vsHash, err := orchestrator.ComputeValSetHash(valSet)
	s.NoError(err)

	genBal := &big.Int{}
	genBal.SetString("999999999999999999999999999999999999999999", 20)

	s.gAlloc = map[ethcmn.Address]core.GenesisAccount{
		s.address: {Balance: genBal},
	}

	s.sim = backends.NewSimulatedBackend(s.gAlloc, 1000000000000000)

	_, _, qgbWrapper, err := wrapper.DeployQuantumGravityBridge(
		s.auth,
		s.sim,
		bID,
		big.NewInt(int64(valSet.Members[0].Power)),
		vsHash,
	)
	s.wrapper = qgbWrapper
	s.Nil(err)
	s.sim.Commit()
}

func (s *QGBTestSuite) TestEncodeValset() {
	vsHash, err := orchestrator.ComputeValSetHash(initialValSet)
	s.NoError(err)
	signBytes := orchestrator.EncodeValsetConfirm(bID, &initialValSet, vsHash)
	signature, err := s.pSigner(s.address, signBytes.Bytes())
	s.NoError(err)

	hexSig := ethcmn.Bytes2Hex(signature)
	ethValSet := []wrapper.Validator{
		{
			Addr:  s.address,
			Power: big.NewInt(5000),
		},
	}

	v, r, ss := orchestrator.SigToVRS(hexSig)
	_, err = s.wrapper.UpdateValidatorSet(
		s.auth,
		big.NewInt(1),
		big.NewInt(5000),
		vsHash,
		ethValSet,
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
}
