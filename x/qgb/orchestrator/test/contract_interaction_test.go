package test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"

	"github.com/tendermint/tendermint/crypto/merkle"
	rpccore "github.com/tendermint/tendermint/rpc/core"

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
	key     *ecdsa.PrivateKey
}

func TestRunQGBSuite(t *testing.T) {
	suite.Run(t, new(QGBTestSuite))
}

func (s *QGBTestSuite) SetupTest() {
	key, err := crypto.HexToECDSA(testPriv)
	s.Require().NoError(err)
	s.key = key

	//nolint
	s.auth = bind.NewKeyedTransactor(key)
	s.auth.GasLimit = 10000000000000
	s.auth.GasPrice = big.NewInt(8750000000)

	valSet := types.Valset{
		Nonce:  0,
		Height: 1,
		Members: []types.BridgeValidator{
			{
				Power:      5000,
				EvmAddress: testAddr,
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

	signature, err := types.NewEthereumSignature(signBytes.Bytes(), s.key)
	s.NoError(err)

	evmVals := make([]wrapper.Validator, len(initialValSet.Members))
	for i, val := range initialValSet.Members {
		evmVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EvmAddress),
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
		evmVals,
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
				EvmAddress: testAddr,
				Power:      5000,
			},
			{
				EvmAddress: testAddr2,
				Power:      5000,
			},
		},
		Nonce:  1,
		Height: 2,
	}

	newVsHash, err := updatedValset.Hash()
	s.NoError(err)
	signBytes, err := updatedValset.SignBytes(bID)
	s.NoError(err)
	signature, err := types.NewEthereumSignature(signBytes.Bytes(), s.key)
	s.NoError(err)

	hexSig := ethcmn.Bytes2Hex(signature)

	evmVals := make([]wrapper.Validator, len(initialValSet.Members))
	for i, val := range initialValSet.Members {
		evmVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EvmAddress),
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
		evmVals,
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

func (s *QGBTestSuite) TestVerifyAttestation() {
	roots, heights, encodedTuples, err := generateRandomDataRootTuples(4)
	s.Require().NoError(err)
	commitment, proofs := merkle.ProofsFromByteSlices(encodedTuples)
	nonce := 1

	signBytes := types.DataCommitmentTupleRootSignBytes(
		bID,
		big.NewInt(int64(nonce)),
		commitment[:],
	)

	signature, err := types.NewEthereumSignature(signBytes.Bytes(), s.key)
	s.NoError(err)

	evmVals := make([]wrapper.Validator, len(initialValSet.Members))
	for i, val := range initialValSet.Members {
		evmVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EvmAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	hexSig := ethcmn.Bytes2Hex(signature)
	v, r, ss := orchestrator.SigToVRS(hexSig)
	tx, err := s.wrapper.SubmitDataRootTupleRoot(
		s.auth,
		big.NewInt(1),
		big.NewInt(0),
		*(*[32]byte)(commitment),
		evmVals,
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
	s.Assert().Equal(0, dcNonce.Cmp(big.NewInt(int64(nonce))))

	err = proofs[1].Verify(commitment, encodedTuples[1])
	s.NoError(err)

	dataRootTuple := wrapper.DataRootTuple{
		Height:   big.NewInt(int64(heights[1])),
		DataRoot: *(*[32]byte)(roots[1]),
	}

	sideNodes := make([][32]byte, len(proofs[1].Aunts))
	for i, p := range proofs[1].Aunts {
		sideNodes[i] = *(*[32]byte)(p)
	}
	wrappedProof := wrapper.BinaryMerkleProof{
		SideNodes: sideNodes,
		Key:       big.NewInt(proofs[1].Index),
		NumLeaves: big.NewInt(proofs[1].Total),
	}

	committedTo, err := s.wrapper.VerifyAttestation(
		&bind.CallOpts{Context: context.TODO()},
		big.NewInt(int64(nonce)),
		dataRootTuple,
		wrappedProof,
	)
	s.NoError(err)
	s.Assert().True(committedTo)
}

// generateRandomDataRootTuples returns a slice of data roots, their corresponding heights
// and the encoded data root tuple.
func generateRandomDataRootTuples(count int) ([][]byte, []uint64, [][]byte, error) {
	heights := make([]uint64, count)
	roots := make([][]byte, count)
	encodedTuples := make([][]byte, count)
	for i := 0; i < count; i++ {
		heights[i] = uint64(i)
		root := bytes.Repeat([]byte{byte(i)}, 32)
		roots[i] = root

		tuple := wrapper.DataRootTuple{
			Height:   big.NewInt(int64(i)),
			DataRoot: *(*[32]byte)(root),
		}
		encodedTuple, err := rpccore.EncodeDataRootTuple(uint64(i), tuple.DataRoot)
		if err != nil {
			return nil, nil, nil, err
		}
		encodedTuples[i] = encodedTuple
	}
	return roots, heights, encodedTuples, nil
}

func (s *QGBTestSuite) updateNonce() error {
	nonce, err := s.sim.NonceAt(context.TODO(), s.auth.From, nil)
	if err != nil {
		return err
	}
	s.auth.Nonce = big.NewInt(int64(nonce))
	return nil
}
