// Prerequisites:
// Deploy docker-compose stack with celestia + evolve reth + hyperlane
//
// Enroll a remote router for the EVM interchain accounts router.
// Note, the destination domain identifier and receiver hex address inputs:
//
//	cast send 0x9F098AE0AC3B7F75F0B3126f471E5F592b47F300 \
//	  "enrollRemoteRouter(uint32,bytes32)" \
//	  69420 0x726f757465725f61707000000000000000000000000000010000000000000000 \
//	  --private-key $HYP_KEY
//	  --rpc-url http://localhost:8545
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	forwardingtypes "github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	// FORCE sdk config to use celestia bech32 encoding (globals...)
	_ "github.com/celestiaorg/celestia-app/v6/app/params"
)

// ABI for only the function we need.
const icaRouterABI = `[
  {
    "inputs": [
      { "internalType": "uint32", "name": "destination", "type": "uint32" },
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

func msgBytes() []byte {
	recipient, err := util.DecodeHexAddress("0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D")
	if err != nil {
		panic(err)
	}

	msg := forwardingtypes.MsgWarpForward{
		DestinationDomain: 5678,
		Recipient:         recipient,
		Token:             sdk.NewCoin("utia", sdkmath.NewInt(1000000)),
	}

	forwardAddr := forwardingtypes.DeriveForwardAddress(msg.DerivationKeys()...)
	fmt.Printf("forwarding address: %s\n", forwardAddr.String())
	fmt.Printf("forwarding address hex: %s\n", hex.EncodeToString(forwardAddr.Bytes()))

	pbAny, err := codectypes.NewAnyWithValue(&msg)
	if err != nil {
		panic(err)
	}

	bz, err := pbAny.Marshal()
	if err != nil {
		panic(err)
	}

	return bz
}

func main() {

	// ---- config you provide ----
	rpcURL := "http://localhost:8545"
	chainID := big.NewInt(1234) // set your chain id

	privateKeyHex := strings.TrimPrefix(os.Getenv("PRIVATE_KEY"), "0x")

	routerAddr := common.HexToAddress("0x9F098AE0AC3B7F75F0B3126f471E5F592b47F300")
	destinationDomain := uint32(69420) // set your destination domain

	// Target contract on destination chain:
	targetAddr := common.HexToAddress("0xTargetContractHere")
	protoBytes := msgBytes()

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

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	nonce, err := client.PendingNonceAt(context.Background(), fromAddr)
	if err != nil {
		log.Fatal("failed to get nonce", err)
	}

	// Estimate gas
	msg := ethereum.CallMsg{
		From: fromAddr,
		To:   &routerAddr,
		Data: data,
	}
	gasLimit, err := client.EstimateGas(context.Background(), msg)
	if err != nil {
		log.Fatal("failed to estimate gas: ", err)
	}

	// Fees (EIP-1559). You can also use SuggestGasPrice for legacy txs.
	tipCap, err := client.SuggestGasTipCap(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}

	// maxFeePerGas = baseFee*2 + tip (simple heuristic)
	feeCap := new(big.Int).Add(new(big.Int).Mul(header.BaseFee, big.NewInt(2)), tipCap)

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

	if err := client.SendTransaction(context.Background(), signedTx); err != nil {
		log.Fatal("failed to send tx", err)
	}

	fmt.Println("tx hash:", signedTx.Hash().Hex())
	fmt.Println("callRemote calldata:", "0x"+hex.EncodeToString(data))
}
