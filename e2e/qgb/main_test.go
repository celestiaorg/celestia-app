package e2e

//
//import (
//	"context"
//	"crypto/ecdsa"
//	"fmt"
//	qgbtypes "github.com/celestiaorg/celestia-app/x/qgb/types"
//	"github.com/cosmos/cosmos-sdk/codec"
//	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
//	sdk "github.com/cosmos/cosmos-sdk/types"
//	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
//	"github.com/tendermint/tendermint/libs/bytes"
//	"google.golang.org/grpc/encoding"
//
//	//sdk "github.com/cosmos/cosmos-sdk/types"
//	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
//	"github.com/ethereum/go-ethereum/accounts/keystore"
//	"github.com/ethereum/go-ethereum/common"
//	"github.com/ethereum/go-ethereum/core/types"
//	"github.com/ethereum/go-ethereum/crypto"
//	"github.com/ethereum/go-ethereum/ethclient"
//	"github.com/status-im/keycard-go/hexutils"
//	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
//	"google.golang.org/grpc"
//
//	tc "github.com/testcontainers/testcontainers-go"
//	"io/ioutil"
//	"math/big"
//	"testing"
//	"time"
//)
//
//func TestSomething(t *testing.T) {
//	// Check for some environemnt variable: INTEGRATION_TEST=true
//
//	identifierFromExistingRunningCompose, err := StartAll()
//	if err != nil {
//		panic(err)
//	}
//	time.Sleep(6000000000)
//	compose := tc.NewLocalDockerCompose(ComposeFilePaths, identifierFromExistingRunningCompose)
//	defer compose.Down()
//	//compose.WithCommand([]string{"down"}).
//	//	Invoke()
//
//	sendEthTransaction()
//	queryCelestiaState()
//	dc := QueryDataCommitments()
//	submitDataCommitmentConfirm(dc)
//
//	//if execError.Error != nil {
//	//	panic(fmt.Errorf("Could not run compose file: %v - %v", ComposeFilePaths, execError.Error))
//	//}
//}
//
//func submitDataCommitmentConfirm(dc bytes.HexBytes) {
//	ctx := context.Background()
//	interfaceRegistry := cdctypes.NewInterfaceRegistry()
//	qgbtypes.RegisterInterfaces(interfaceRegistry)
//	c := codec.NewProtoCodec(interfaceRegistry)
//	print(len(c.InterfaceRegistry().ListAllInterfaces()))
//	ccc := encoding.GetCodec("proto")
//	print(ccc.Name())
//	//
//	//cc := encoding.
//
//	grpcConn, err := grpc.Dial(
//		"192.168.11.2:9090", // your gRPC server address.
//		grpc.WithInsecure(), // The Cosmos SDK doesn't support any transport security mechanism.
//		// This instantiates a general gRPC codec which handles proto bytes. We pass in a nil interface registry
//		// if the request/response types contain interface instead of 'nil' you should pass the application specific codec.
//		//grpc.WithDefaultCallOptions(),
//		//grpc.WithDefaultCallOptions(),
//		grpc.WithDefaultCallOptions(grpc.ForceCodec(ccc)),
//	)
//	if err != nil {
//		print(err.Error())
//		return
//	}
//	defer grpcConn.Close()
//
//	// This creates a gRPC client to query the x/bank service.
//	qgbClient := qgbtypes.NewMsgClient(grpcConn)
//
//	validatorAddress, _ := sdk.AccAddressFromHex("celes1qqan0fj73dhqfgwkuuh4qj2e77g4tugkrhy2jn")
//	ethAddress := stakingtypes.EthAddress{}
//	ethAddress.SetAddress("0x123")
//	req := qgbtypes.NewMsgDataCommitmentConfirm(dc.String(), "", validatorAddress, ethAddress, int64(1), int64(5))
//	resp, err := qgbClient.DataCommitmentConfirm(ctx, req)
//	if err != nil {
//		print(err.Error())
//		return
//	}
//	print(resp.Size())
//}
//
//func QueryDataCommitments() bytes.HexBytes {
//	ctx := context.Background()
//	client, err := rpchttp.New("http://192.168.11.2:26657", "/websocket")
//	if err != nil {
//		fmt.Println(err.Error())
//		return nil
//	}
//	height := int64(1)
//	block, err := client.Block(ctx, &height)
//	if err != nil {
//		fmt.Println(err.Error())
//		return nil
//	}
//	print(block.BlockID.String())
//
//	dc1, err := client.DataCommitment(ctx, fmt.Sprintf("block.height <= 3"))
//	if err != nil {
//		fmt.Println(err.Error())
//		return nil
//	}
//	print(dc1.DataCommitment.String())
//
//	dc2, err := client.DataCommitment(ctx, fmt.Sprintf("block.height <= 6"))
//	if err != nil {
//		fmt.Println(err.Error())
//		return nil
//	}
//	print(dc2.DataCommitment.String())
//	return dc2.DataCommitment
//}
//
//func queryCelestiaState() error {
//	//myAddress, err := sdk.AccAddressFromBech32("cosmos1...")
//	//if err != nil {
//	//	return err
//	//}
//
//	// Create a connection to the gRPC server.
//	grpcConn, err := grpc.Dial(
//		"192.168.11.2:9090", // your gRPC server address.
//		grpc.WithInsecure(), // The Cosmos SDK doesn't support any transport security mechanism.
//		// This instantiates a general gRPC codec which handles proto bytes. We pass in a nil interface registry
//		// if the request/response types contain interface instead of 'nil' you should pass the application specific codec.
//		grpc.WithDefaultCallOptions(),
//		//grpc.WithDefaultCallOptions(grpc.ForceCodec(codec.NewProtoCodec(nil).GRPCCodec())),
//	)
//	defer grpcConn.Close()
//
//	// This creates a gRPC client to query the x/bank service.
//	bankClient := banktypes.NewQueryClient(grpcConn)
//	bankRes, err := bankClient.Balance(
//		context.Background(),
//		&banktypes.QueryBalanceRequest{Address: "celes1qqan0fj73dhqfgwkuuh4qj2e77g4tugkrhy2jn", Denom: "celes"},
//	)
//	if err != nil {
//		return err
//	}
//	balance := bankRes.GetBalance()
//
//	fmt.Println(balance) // Prints the account balance
//
//	return nil
//}
//
//func sendEthTransaction() {
//	ctx := context.Background()
//
//	inPath := "./ethereum/keystore/UTC--2022-03-23T10-28-01.551603386Z--e7744223cdb1d7e558d6a35155b658678ec19de2"
//	password := "password"
//	keyjson, e := ioutil.ReadFile(inPath)
//	if e != nil {
//		panic(e)
//	}
//	key, e := keystore.DecryptKey(keyjson, password)
//	if e != nil {
//		panic(e)
//	}
//
//	privateKey := key.PrivateKey
//	//another := new(big.Int)
//	//another.SetString("0x1ebff46d79a475ef795be8e8edeb8f7906270af8ad26ba9231b552c3f48c91f9", 16)
//	//anoother := new(big.Int)
//	//anoother.SetString(privateKey.D.String(), 10)
//
//	anotherPriv, err := crypto.ToECDSA(hexutils.HexToBytes("1ebff46d79a475ef795be8e8edeb8f7906270af8ad26ba9231b552c3f48c91f9"))
//	equal := privateKey.Equal(anotherPriv)
//	print(equal)
//	publicKey := privateKey.Public()
//	publicKeyECDSA, _ := publicKey.(*ecdsa.PublicKey)
//
//	//anotherPublic := another.
//	// Function requires the public address of the account we're sending from -- which we can derive from the private key.
//	//}
//	//	panic(e)
//	//if e != nil {
//	//e = crypto.SaveECDSA(outPath, key.PrivateKey)
//	//}
//	//	panic(err)
//	//if err != nil {
//	//privateKey, err := crypto.ToECDSA([]byte("0x1ebff46d79a475ef795be8e8edeb8f7906270af8ad26ba9231b552c3f48c91f9"))
//	//privateKey, err := crypto.LoadECDSA("ethereum/keystore/UTC--2022-03-23T10-28-01.551603386Z--e7744223cdb1d7e558d6a35155b658678ec19de2")
//
//	client, err := ethclient.Dial("http://192.168.11.6:8545")
//	if err != nil {
//		fmt.Println("Oops! There was a problem", err)
//	} else {
//		fmt.Println("Success! you are connected to the Ethereum Network")
//	}
//
//	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)
//
//	toAddress := common.HexToAddress("0x123")
//
//	blockHeight, err := client.BlockNumber(ctx)
//	bigInt := big.NewInt(int64(blockHeight))
//	nonce, err := client.NonceAt(ctx, fromAddress, bigInt)
//	if err != nil {
//		panic(err)
//	}
//	var data []byte
//	tx := types.NewTransaction(nonce, toAddress, big.NewInt(int64(12300000)), uint64(90071), big.NewInt(int64(2000000)), data)
//	chainID, err := client.NetworkID(context.Background())
//	if err != nil {
//		panic(err)
//	}
//
//	// We sign the transaction using the sender's private key
//	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
//	if err != nil {
//		panic(err)
//	}
//
//	currentBalance, err := client.BalanceAt(ctx, fromAddress, bigInt)
//	print(currentBalance.String())
//	// Now we are finally ready to broadcast the transaction to the entire network
//	err = client.SendTransaction(context.Background(), signedTx)
//	if err != nil {
//		panic(err)
//	}
//
//	// We return the transaction hash
//	hash := signedTx.Hash().String()
//
//	print(hash)
//	//err = client.SendTransaction(ctx, tx)
//}
