package test

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/keystore"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/suite"
)

var (
	bID           = ethcmn.HexToHash("0x13370000000000000000000000000000")
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
	s.auth.GasPrice = big.NewInt(8750000000)
	// s.auth.GasFeeCap = big.NewInt(999999999999999999)
	// s.auth.GasTipCap = big.NewInt(999999999999999999)
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

	// initialCheckpoint, err := orchestrator.ValsetSignBytes(bID, initialValSet)
	// s.NoError(err)

	genBal := &big.Int{}
	genBal.SetString("999999999999999999999999999999999999999999", 20)

	s.gAlloc = map[ethcmn.Address]core.GenesisAccount{
		s.address: {Balance: genBal},
	}

	s.sim = backends.NewSimulatedBackend(s.gAlloc, 1000000000000000)

	contractAddress, _, qgbWrapper, err := wrapper.DeployQuantumGravityBridge(
		s.auth,
		s.sim,
		bID,
		big.NewInt(int64(initialValSet.TwoThirdsThreshold())),
		vsHash,
	)
	fmt.Println("qgb contract deployed", contractAddress.Hex())
	s.NoError(err)
	s.wrapper = qgbWrapper

	s.sim.Commit()

	cbid, err := qgbWrapper.BRIDGEID(nil)
	s.NoError(err)
	s.Require().Equal(bID.Hex(), "0x"+ethcmn.Bytes2Hex(cbid[:]))
}

func (s *QGBTestSuite) TestEncodeValset() {
	vsHash, err := orchestrator.ComputeValSetHash(initialValSet)
	s.NoError(err)
	signBytes, err := orchestrator.ValsetSignBytes(bID, initialValSet)
	s.NoError(err)
	signature, err := s.pSigner(s.address, signBytes.Bytes())
	s.NoError(err)

	hexSig := ethcmn.Bytes2Hex(signature)
	ethValSet := []wrapper.Validator{
		{
			Addr:  s.address,
			Power: big.NewInt(5000),
		},
	}

	s.NoError(err)

	v, r, ss := orchestrator.SigToVRS(hexSig)
	tx, err := s.wrapper.UpdateValidatorSet(
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

	// msg := ethereum.CallMsg{
	// 	From:     s.auth.From,
	// 	To:       tx.To(),
	// 	Gas:      tx.Gas(),
	// 	GasPrice: tx.GasPrice(),
	// 	// GasFeeCap: tx.GasFeeCap(),
	// 	// GasTipCap: tx.GasTipCap(),
	// 	Value: tx.Value(),
	// 	Data:  tx.Data(),
	// }

	// _, err = s.sim.EstimateGas(context.Background(), msg)
	// if err != nil {
	// 	fmt.Println("********************", err)
	// }

	reason, err := s.errorReason(context.Background(), tx, big.NewInt(int64(1)))
	fmt.Println("*********************", reason, err)

	s.sim.Commit()

	recp, err := s.sim.TransactionReceipt(context.TODO(), tx.Hash())
	s.NoError(err)
	s.Require().Equal(uint64(1), recp.Status)
	fmt.Println("logs", recp.Logs, recp.GasUsed, recp.BlockNumber, recp.Status)
	fmt.Printf("%+v\n", recp)

	// block := s.sim.Blockchain().GetBlockByNumber(2)
	// fmt.Println(s.sim.ev)

	valSetNonce, err := s.wrapper.StateLastValidatorSetNonce(nil)
	s.NoError(err)

	fmt.Println(valSetNonce.String())

	s.Equal(0, valSetNonce.Cmp(big.NewInt(1)))
}

func (s *QGBTestSuite) updateNonce() error {
	nonce, err := s.sim.NonceAt(context.TODO(), s.address, nil)
	if err != nil {
		return err
	}
	fmt.Println("updated nonce", nonce)
	s.auth.Nonce = big.NewInt(int64(nonce + 1))
	return nil
}

// func (s *QGBTestSuite) TestDataCommitmentEncoding() {
// 	orchestrator.EncodeDataCommitmentConfirm(bID, big.NewInt(1), []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
// }

func (s *QGBTestSuite) errorReason(ctx context.Context, tx *ethtypes.Transaction, blockNum *big.Int) (string, error) {
	msg := ethereum.CallMsg{
		From:     s.auth.From,
		To:       tx.To(),
		Gas:      tx.Gas(),
		GasPrice: tx.GasPrice(),
		Value:    tx.Value(),
		Data:     tx.Data(),
	}
	res, err := s.sim.CallContract(ctx, msg, blockNum)
	if err != nil {
		return "", errors.Wrap(err, "CallContract")
	}
	return unpackError(res)
}

var (
	errorSig     = []byte{0x08, 0xc3, 0x79, 0xa0} // Keccak256("Error(string)")[:4]
	abiString, _ = abi.NewType("string", "", nil)
)

func unpackError(result []byte) (string, error) {
	if len(result) < 4 || !bytes.Equal(result[:4], errorSig) {
		return "<tx result not Error(string)>", errors.New("TX result not of type Error(string)")
	}
	vs, err := abi.Arguments{{Type: abiString}}.UnpackValues(result[4:])
	if err != nil {
		return "<invalid tx result>", errors.Wrap(err, "unpacking revert reason")
	}
	return vs[0].(string), nil
}
