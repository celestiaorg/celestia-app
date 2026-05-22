package ethrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyMethods(t *testing.T) {
	celAddr := sdk.AccAddress("celestia-account-addr")
	ethAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	ctx := sdk.Context{}.WithBlockHeight(42).WithBlockTime(time.Unix(123, 0))
	account := authtypes.NewBaseAccountWithAddress(celAddr)
	require.NoError(t, account.SetSequence(7))

	server := NewServer(Config{
		ContextProvider: func() (sdk.Context, error) {
			return ctx, nil
		},
		ChainIDProvider: func() string {
			return "celestiadev"
		},
		GasPriceProvider: func() (float64, error) {
			return 4.2, nil
		},
		IdentityKeeper: mockIdentityKeeper{
			mappings: map[common.Address]sdk.AccAddress{ethAddr: celAddr},
		},
		AccountKeeper: mockAccountKeeper{
			accounts: map[string]sdk.AccountI{celAddr.String(): account},
		},
		BankKeeper: mockBankKeeper{
			balances: map[string]sdk.Coin{
				celAddr.String(): sdk.NewCoin("utia", sdkmath.NewInt(1234)),
			},
		},
		ClientVersion: "celestia-app/test",
	})

	tests := []struct {
		name   string
		method string
		params string
		want   any
	}{
		{name: "eth_chainId", method: "eth_chainId", want: "0x3039"},
		{name: "net_version", method: "net_version", want: "12345"},
		{name: "web3_clientVersion", method: "web3_clientVersion", want: "celestia-app/test"},
		{name: "eth_blockNumber", method: "eth_blockNumber", want: "0x2a"},
		{name: "eth_getBalance", method: "eth_getBalance", params: `["` + ethAddr.Hex() + `","latest"]`, want: "0x4625103a72000"},
		{name: "eth_getTransactionCount", method: "eth_getTransactionCount", params: `["` + ethAddr.Hex() + `","latest"]`, want: "0x7"},
		{name: "eth_gasPrice", method: "eth_gasPrice", want: "0x48c27395000"},
		{name: "eth_maxPriorityFeePerGas", method: "eth_maxPriorityFeePerGas", want: "0x0"},
		{name: "eth_syncing", method: "eth_syncing", want: false},
		{name: "net_listening", method: "net_listening", want: true},
		{name: "net_peerCount", method: "net_peerCount", want: "0x0"},
		{name: "eth_getCode", method: "eth_getCode", params: `["` + ethAddr.Hex() + `","latest"]`, want: "0x"},
		{name: "eth_call", method: "eth_call", params: `[{"to":"` + ethAddr.Hex() + `"},"latest"]`, want: "0x"},
		{name: "eth_estimateGas", method: "eth_estimateGas", params: `[{"to":"` + ethAddr.Hex() + `"}]`, want: "0x186a0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := requestRPC(t, server, tc.method, tc.params)
			require.Nil(t, resp.Error)
			require.Equal(t, tc.want, resp.Result)
		})
	}
}

func TestGetBlockByNumber(t *testing.T) {
	server := newZeroValueServer()
	resp := requestRPC(t, server, "eth_getBlockByNumber", `["latest",false]`)
	require.Nil(t, resp.Error)

	block, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "0x1", block["number"])
	require.Equal(t, "0x0", block["gasUsed"])
	require.Equal(t, []any{}, block["transactions"])
	require.Equal(t, []any{}, block["uncles"])
	require.Equal(t, "0x0", block["baseFeePerGas"])
}

func TestFeeHistory(t *testing.T) {
	server := newZeroValueServer()
	server.gasPriceProvider = func() (float64, error) { return 2, nil }

	resp := requestRPC(t, server, "eth_feeHistory", `["0x2","latest",[10,50]]`)
	require.Nil(t, resp.Error)

	history, ok := resp.Result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "0x0", history["oldestBlock"])
	require.Equal(t, []any{"0x1d1a94a2000", "0x1d1a94a2000", "0x1d1a94a2000"}, history["baseFeePerGas"])
	require.Equal(t, []any{float64(0), float64(0)}, history["gasUsedRatio"])
	require.Equal(t, []any{[]any{"0x0", "0x0"}, []any{"0x0", "0x0"}}, history["reward"])
}

func TestUnresolvedAddressReturnsZeroBalanceAndTransactionCount(t *testing.T) {
	server := newZeroValueServer()
	ethAddr := "0x2222222222222222222222222222222222222222"

	balanceResp := requestRPC(t, server, "eth_getBalance", `["`+ethAddr+`","latest"]`)
	require.Nil(t, balanceResp.Error)
	require.Equal(t, "0x0", balanceResp.Result)

	txCountResp := requestRPC(t, server, "eth_getTransactionCount", `["`+ethAddr+`","latest"]`)
	require.Nil(t, txCountResp.Error)
	require.Equal(t, "0x0", txCountResp.Result)
}

func TestBatchRequest(t *testing.T) {
	server := newZeroValueServer()
	body := []byte(`[
		{"jsonrpc":"2.0","id":"chain","method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":"net","method":"net_version","params":[]}
	]`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var responses []rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &responses))
	require.Len(t, responses, 2)

	results := make(map[string]any, len(responses))
	for _, resp := range responses {
		require.Nil(t, resp.Error)
		var id string
		require.NoError(t, json.Unmarshal(resp.ID, &id))
		results[id] = resp.Result
	}
	require.Equal(t, "0x3039", results["chain"])
	require.Equal(t, "12345", results["net"])
}

func TestSendRawTransactionBroadcastsTranslatedValueTransfer(t *testing.T) {
	rawTx, txHash, from, to := signedDynamicFeeTx(t)
	fromCelestia := sdk.AccAddress("sender-celestia-account")
	toCelestia := sdk.AccAddress("recipient-celestia-acct")
	account := authtypes.NewBaseAccountWithAddress(fromCelestia)
	require.NoError(t, account.SetSequence(7))
	var broadcastedTx []byte

	server := NewServer(Config{
		ContextProvider: func() (sdk.Context, error) {
			return sdk.Context{}.WithBlockHeight(1), nil
		},
		ChainIDProvider: func() string {
			return "celestiadev"
		},
		GasPriceProvider: func() (float64, error) {
			return 0, nil
		},
		TxBroadcaster: func(txBytes []byte) (*sdk.TxResponse, error) {
			broadcastedTx = append([]byte(nil), txBytes...)
			return &sdk.TxResponse{Code: 0, TxHash: "CELESTIA_TX_HASH"}, nil
		},
		IdentityKeeper: mockIdentityKeeper{
			mappings: map[common.Address]sdk.AccAddress{
				from: fromCelestia,
				to:   toCelestia,
			},
		},
		AccountKeeper: mockAccountKeeper{
			accounts: map[string]sdk.AccountI{fromCelestia.String(): account},
		},
		BankKeeper:    mockBankKeeper{},
		ClientVersion: "celestia-app/test",
	})

	translation, err := server.translateEthereumValueTransfer(rawTx)
	require.NoError(t, err)
	require.Equal(t, txHash.Hex(), translation.EthereumTxHash)
	require.Equal(t, from.Hex(), translation.FromEthereumAddress)
	require.Equal(t, to.Hex(), translation.ToEthereumAddress)
	require.Equal(t, fromCelestia.String(), translation.FromCelestiaAddress)
	require.Equal(t, toCelestia.String(), translation.ToCelestiaAddress)
	require.Equal(t, "1000000utia", translation.Amount)
	require.Equal(t, "42utia", translation.FeeAmount)
	require.Equal(t, uint64(21000), translation.GasLimit)
	require.Equal(t, uint64(7), translation.Sequence)
	require.NotEmpty(t, translation.BodyBytesHash)
	require.NotEmpty(t, translation.AuthInfoBytesHash)
	require.Positive(t, translation.TxRawBytesLen)
	require.NotEmpty(t, translation.TxRawBytes)
	enc := encoding.MakeConfig(bank.AppModuleBasic{})
	_, err = enc.TxConfig.TxDecoder()(translation.TxRawBytes)
	require.NoError(t, err)

	resp := requestRPC(t, server, "eth_sendRawTransaction", `["`+hexutil.Encode(rawTx)+`"]`)
	require.Nil(t, resp.Error)
	require.Equal(t, txHash.Hex(), resp.Result)
	require.Equal(t, translation.TxRawBytes, broadcastedTx)
}

func TestSendRawTransactionRejectsUnresolvedSender(t *testing.T) {
	rawTx, _, _, _ := signedDynamicFeeTx(t)

	resp := requestRPC(t, newZeroValueServer(), "eth_sendRawTransaction", `["`+hexutil.Encode(rawTx)+`"]`)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32000, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "unresolved ethereum sender")
}

func TestSendRawTransactionReturnsBroadcastRejection(t *testing.T) {
	rawTx, _, from, to := signedDynamicFeeTx(t)
	fromCelestia := sdk.AccAddress("sender-celestia-account")
	toCelestia := sdk.AccAddress("recipient-celestia-acct")
	account := authtypes.NewBaseAccountWithAddress(fromCelestia)
	require.NoError(t, account.SetSequence(7))

	server := NewServer(Config{
		ContextProvider: func() (sdk.Context, error) {
			return sdk.Context{}.WithBlockHeight(1), nil
		},
		ChainIDProvider: func() string {
			return "celestiadev"
		},
		GasPriceProvider: func() (float64, error) {
			return 0, nil
		},
		TxBroadcaster: func(_ []byte) (*sdk.TxResponse, error) {
			return &sdk.TxResponse{Code: 4, Codespace: "sdk", RawLog: "unauthorized"}, nil
		},
		IdentityKeeper: mockIdentityKeeper{
			mappings: map[common.Address]sdk.AccAddress{
				from: fromCelestia,
				to:   toCelestia,
			},
		},
		AccountKeeper: mockAccountKeeper{
			accounts: map[string]sdk.AccountI{fromCelestia.String(): account},
		},
		BankKeeper:    mockBankKeeper{},
		ClientVersion: "celestia-app/test",
	})

	resp := requestRPC(t, server, "eth_sendRawTransaction", `["`+hexutil.Encode(rawTx)+`"]`)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32000, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "unauthorized")
}

func TestUnknownChainIDReturnsInternalError(t *testing.T) {
	server := newZeroValueServer()
	server.chainIDProvider = func() string { return "unknown-chain" }

	resp := requestRPC(t, server, "eth_chainId", "")
	require.NotNil(t, resp.Error)
	require.Equal(t, -32000, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "no Ethereum chain ID mapping")
}

func TestRegisterRoutesHandlesPostRoot(t *testing.T) {
	router := mux.NewRouter()
	RegisterRoutes(router, newZeroValueServer())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRegisterRoutesHandlesGetHealthCheck(t *testing.T) {
	router := mux.NewRouter()
	RegisterRoutes(router, newZeroValueServer())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func requestRPC(t *testing.T, server *Server, method string, params string) rpcResponse {
	t.Helper()
	if params == "" {
		params = "[]"
	}
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result"`
	Error   *jsonRPCError   `json:"error"`
	ID      json.RawMessage `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newZeroValueServer() *Server {
	return NewServer(Config{
		ContextProvider: func() (sdk.Context, error) {
			return sdk.Context{}.WithBlockHeight(1), nil
		},
		ChainIDProvider: func() string {
			return "celestiadev"
		},
		GasPriceProvider: func() (float64, error) {
			return 0, nil
		},
		IdentityKeeper: mockIdentityKeeper{},
		AccountKeeper:  mockAccountKeeper{},
		BankKeeper:     mockBankKeeper{},
		ClientVersion:  "celestia-app/test",
	})
}

func signedDynamicFeeTx(t *testing.T) ([]byte, common.Hash, common.Address, common.Address) {
	t.Helper()

	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	from := gethcrypto.PubkeyToAddress(key.PublicKey)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(12345),
		Nonce:     7,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1_000_000_000_000_000_000),
	})
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(12345)), key)
	require.NoError(t, err)
	rawTx, err := signedTx.MarshalBinary()
	require.NoError(t, err)
	return rawTx, signedTx.Hash(), from, to
}

type mockIdentityKeeper struct {
	mappings map[common.Address]sdk.AccAddress
}

func (k mockIdentityKeeper) Resolve(_ sdk.Context, ethAddr []byte) (sdk.AccAddress, bool) {
	addr, found := k.mappings[common.BytesToAddress(ethAddr)]
	return addr, found
}

type mockAccountKeeper struct {
	accounts map[string]sdk.AccountI
}

func (k mockAccountKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return k.accounts[addr.String()]
}

type mockBankKeeper struct {
	balances map[string]sdk.Coin
}

func (k mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if coin, found := k.balances[addr.String()]; found {
		return coin
	}
	return sdk.NewCoin(denom, sdkmath.ZeroInt())
}

func TestEthereumChainIDForCelestia(t *testing.T) {
	tests := map[string]uint64{
		"celestiadev": DevEthereumChainID,
		"test":        DevEthereumChainID,
		"test-app":    DevEthereumChainID,
		"celestia":    mainnetEthereumChainID,
		"mocha-4":     mochaEthereumChainID,
		"arabica-11":  arabicaEthereumChainID,
	}

	for chainID, want := range tests {
		got, err := EthereumChainIDForCelestia(chainID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestCeilGasPrice(t *testing.T) {
	require.Equal(t, uint64(0), ceilGasPrice(0))
	require.Equal(t, uint64(1), ceilGasPrice(0.004))
	require.Equal(t, uint64(2), ceilGasPrice(2))
	require.Equal(t, uint64(3), ceilGasPrice(2.1))
	require.Equal(t, maxUint64, ceilGasPrice(float64(maxUint64)*2))
}

func TestHexQuantityFormatting(t *testing.T) {
	require.Equal(t, "0x0", hexQuantityUint64(0))
	require.Equal(t, "0xff", hexQuantityUint64(255))
	require.Equal(t, "0x0", hexQuantityBigInt(nil))
	require.Equal(t, "0x100", hexQuantityBigInt(big.NewInt(256)))
}

func TestToEVMUnits(t *testing.T) {
	require.Equal(t, "0x0", hexQuantityBigInt(toEVMUnits(nil)))
	require.Equal(t, "0x0", hexQuantityBigInt(toEVMUnits(big.NewInt(0))))
	require.Equal(t, "0xde0b6b3a7640000", hexQuantityBigInt(toEVMUnits(big.NewInt(1_000_000))))
}

func TestFromEVMUnits(t *testing.T) {
	amount, err := fromEVMUnits(big.NewInt(1_000_000_000_000_000_000))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(1_000_000), amount)

	_, err = fromEVMUnits(big.NewInt(1))
	require.Error(t, err)
}
