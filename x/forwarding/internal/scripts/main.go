package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ABI for only the function we need.
const icaRouterABI = `[
  {
    "inputs": [
      { "internalType": "uint32", "name": "destinationDomain", "type": "uint32" },
      {
        "components": [
          { "internalType": "bytes32", "name": "to", "type": "bytes32" },
          { "internalType": "uint256", "name": "value", "type": "uint256" },
          { "internalType": "bytes", "name": "data", "type": "bytes" }
        ],
        "internalType": "struct CallLib.Call[]",
        "name": "calls",
        "type": "tuple[]"
      }
    ],
    "name": "callRemote",
    "outputs": [{ "internalType": "bytes32", "name": "", "type": "bytes32" }],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

// Call matches the tuple (bytes32,uint256,bytes)
type Call struct {
	To    [32]byte
	Value *big.Int
	Data  []byte
}

// addressToBytes32 left-pads an EVM address into bytes32 (same as Hyperlane TypeCasts.addressToBytes32)
func addressToBytes32(addr common.Address) [32]byte {
	var out [32]byte
	copy(out[12:], addr.Bytes()) // right-align 20 bytes
	return out
}

func mustDecodeHex0x(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	// ---- config you provide ----
	rpcURL := "https://localhost:8545"
	chainID := big.NewInt(1234) // set your chain id

	privateKeyHex := "YOUR_PRIVATE_KEY_HEX_NO_0x"

	routerAddr := common.HexToAddress("0xRouterAddressHere")
	destinationDomain := uint32(1234) // set your destination domain

	// Target contract on destination chain (EVM):
	targetAddr := common.HexToAddress("0xTargetContractHere")
	// Example: arbitrary bytes you want to deliver (protobuf blob, etc.)
	protoBytes := mustDecodeHex0x("0xdeadbeef")

	calls := []Call{
		{
			To:    addressToBytes32(targetAddr),
			Value: big.NewInt(0),
			Data:  protoBytes,
		},
	}

	// ---- pack callRemote calldata ----
	routerABI, err := abi.JSON(strings.NewReader(icaRouterABI))
	if err != nil {
		log.Fatal(err)
	}

	// IMPORTANT: the second argument type is tuple[], and our []Call matches it.
	data, err := routerABI.Pack("callRemote", destinationDomain, calls)
	if err != nil {
		log.Fatal(err)
	}

	// ---- build & sign tx ----
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatal(err)
	}

	pubKey, ok := privKey.Public().(ecdsa.PublicKey)
	if !ok {
		log.Fatal("failed to convert ecdsa public key")
	}

	fromAddr := crypto.PubkeyToAddress(pubKey)

	nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
	if err != nil {
		log.Fatal(err)
	}

	// Estimate gas
	msg := ethereum.CallMsg{
		From: fromAddr,
		To:   &routerAddr,
		Data: data,
	}
	gasLimit, err := client.EstimateGas(context.Background(), msg)
	if err != nil {
		log.Fatal(err)
	}

	// Fees (EIP-1559). You can also use SuggestGasPrice for legacy txs.
	tipCap, err := client.SuggestGasTipCap(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	head, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	// maxFeePerGas = baseFee*2 + tip (simple heuristic)
	feeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tipCap)

	// 0 ETH value to router (router pays nothing by default; IGP is separate)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		To:        &routerAddr,
		Value:     big.NewInt(0),
		Gas:       gasLimit,
		GasTipCap: tipCap,
		GasFeeCap: feeCap,
		Data:      data,
	})

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, privKey)
	if err != nil {
		log.Fatal(err)
	}

	// Send it
	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		log.Fatal(err)
	}

	fmt.Println("tx hash:", signedTx.Hash().Hex())
	fmt.Println("callRemote calldata:", "0x"+hex.EncodeToString(data))
}
