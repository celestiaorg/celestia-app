package ethrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"

	sdklog "cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	txethereum "github.com/celestiaorg/celestia-app/v9/pkg/tx/ethereum"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txv1beta1 "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/mux"
)

const maxUint64 = ^uint64(0)

const (
	defaultEstimateGas = 100000
	nativeDecimals     = 6
	evmDecimals        = 18
	emptyUncleHash     = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	emptyRootHash      = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
)

var evmUnitMultiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(evmDecimals-nativeDecimals), nil)

var errSendRawTransactionBroadcastDisabled = errors.New("eth_sendRawTransaction translated successfully, but broadcast is not configured")

// ContextProvider returns the latest committed query context.
type ContextProvider func() (sdk.Context, error)

// ChainIDProvider returns the active Celestia chain ID.
type ChainIDProvider func() string

// GasPriceProvider returns the native minimum gas price in utia per gas.
type GasPriceProvider func() (float64, error)

// TxBroadcaster submits encoded Celestia transaction bytes to the normal
// transaction broadcast path.
type TxBroadcaster func(txBytes []byte) (*sdk.TxResponse, error)

// EthIdentityKeeper resolves observed Ethereum identities to canonical Celestia
// accounts.
type EthIdentityKeeper interface {
	Resolve(ctx sdk.Context, ethAddr []byte) (sdk.AccAddress, bool)
}

// AccountKeeper reads canonical Celestia accounts.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// BankKeeper reads native Celestia balances.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// Server handles Ethereum JSON-RPC wallet display methods and the supported
// transaction broadcast adapter.
type Server struct {
	contextProvider  ContextProvider
	chainIDProvider  ChainIDProvider
	gasPriceProvider GasPriceProvider
	txBroadcaster    TxBroadcaster
	identityKeeper   EthIdentityKeeper
	accountKeeper    AccountKeeper
	bankKeeper       BankKeeper
	clientVersion    string
	logger           sdklog.Logger
	rpc              *gethrpc.Server
}

// Config defines dependencies for the Ethereum JSON-RPC server.
type Config struct {
	ContextProvider  ContextProvider
	ChainIDProvider  ChainIDProvider
	GasPriceProvider GasPriceProvider
	TxBroadcaster    TxBroadcaster
	IdentityKeeper   EthIdentityKeeper
	AccountKeeper    AccountKeeper
	BankKeeper       BankKeeper
	ClientVersion    string
	Logger           sdklog.Logger
}

// NewServer creates an Ethereum JSON-RPC compatibility server.
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = sdklog.NewNopLogger()
	}
	server := &Server{
		contextProvider:  cfg.ContextProvider,
		chainIDProvider:  cfg.ChainIDProvider,
		gasPriceProvider: cfg.GasPriceProvider,
		txBroadcaster:    cfg.TxBroadcaster,
		identityKeeper:   cfg.IdentityKeeper,
		accountKeeper:    cfg.AccountKeeper,
		bankKeeper:       cfg.BankKeeper,
		clientVersion:    cfg.ClientVersion,
		logger:           logger.With("module", "ethrpc"),
	}
	server.rpc = gethrpc.NewServer()
	mustRegister(server.rpc, "eth", ethService{server: server})
	mustRegister(server.rpc, "net", netService{server: server})
	mustRegister(server.rpc, "web3", web3Service{server: server})
	return server
}

// RegisterRoutes mounts the Ethereum JSON-RPC handler on /.
func RegisterRoutes(router *mux.Router, server *Server) {
	router.Handle("/", server).Methods(http.MethodGet, http.MethodOptions, http.MethodPost)
}

// ServeHTTP handles Ethereum JSON-RPC requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.rpc.ServeHTTP(w, req)
}

type ethService struct {
	server *Server
}

type netService struct {
	server *Server
}

type web3Service struct {
	server *Server
}

func mustRegister(server *gethrpc.Server, namespace string, service any) {
	if err := server.RegisterName(namespace, service); err != nil {
		panic(err)
	}
}

func (s ethService) ChainId() (string, error) {
	chainID, err := s.ethereumChainID()
	if err != nil {
		return "", err
	}
	return hexQuantityUint64(chainID), nil
}

func (s netService) Version() (string, error) {
	chainID, err := s.ethereumChainID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", chainID), nil
}

func (s web3Service) ClientVersion() string {
	return s.server.clientVersion
}

func (s ethService) Syncing() bool {
	return false
}

func (s netService) Listening() bool {
	return true
}

func (s netService) PeerCount() string {
	return "0x0"
}

func (s ethService) BlockNumber() (string, error) {
	ctx, err := s.contextProvider()
	if err != nil {
		return "", err
	}
	return hexQuantityInt64(ctx.BlockHeight()), nil
}

func (s ethService) GetBlockByNumber(blockNumber string, _ *bool) (any, error) {
	ctx, err := s.contextProvider()
	if err != nil {
		return nil, err
	}

	if blockNumber != "latest" && blockNumber != "safe" && blockNumber != "finalized" && blockNumber != hexQuantityInt64(ctx.BlockHeight()) {
		return nil, nil
	}
	return s.block(ctx), nil
}

func (s ethService) GetBlockByHash(_ common.Hash, _ *bool) any {
	return nil
}

func (s ethService) GetBalance(addr common.Address, _ string) (string, error) {
	balance, err := s.balance(addr)
	if err != nil {
		return "", err
	}
	return hexQuantityBigInt(balance), nil
}

func (s ethService) GetTransactionCount(addr common.Address, _ string) (string, error) {
	sequence, err := s.transactionCount(addr)
	if err != nil {
		return "", err
	}
	return hexQuantityUint64(sequence), nil
}

func (s ethService) GasPrice() (string, error) {
	gasPrice, err := s.gasPriceProvider()
	if err != nil {
		return "", err
	}
	return hexQuantityBigInt(toEVMUnits(new(big.Int).SetUint64(ceilGasPrice(gasPrice)))), nil
}

func (s ethService) MaxPriorityFeePerGas() string {
	return "0x0"
}

func (s ethService) FeeHistory(blockCount hexutil.Uint64, _ string, rewardPercentiles *[]float64) (map[string]any, error) {
	ctx, err := s.contextProvider()
	if err != nil {
		return nil, err
	}

	count := uint64(blockCount)
	if count == 0 {
		count = 1
	}
	if count > 1024 {
		count = 1024
	}

	gasPrice := uint64(0)
	if price, err := s.gasPriceProvider(); err == nil {
		gasPrice = ceilGasPrice(price)
	}
	evmGasPrice := hexQuantityBigInt(toEVMUnits(new(big.Int).SetUint64(gasPrice)))

	oldest := uint64(0)
	if ctx.BlockHeight() > 0 {
		current := uint64(ctx.BlockHeight())
		if current >= count {
			oldest = current - count + 1
		}
	}

	baseFeePerGas := make([]string, count+1)
	gasUsedRatio := make([]float64, count)
	for i := range baseFeePerGas {
		baseFeePerGas[i] = evmGasPrice
	}

	result := map[string]any{
		"oldestBlock":   hexQuantityUint64(oldest),
		"baseFeePerGas": baseFeePerGas,
		"gasUsedRatio":  gasUsedRatio,
	}

	if rewardPercentiles != nil {
		reward := make([][]string, count)
		for i := range reward {
			reward[i] = make([]string, len(*rewardPercentiles))
			for j := range reward[i] {
				reward[i][j] = "0x0"
			}
		}
		result["reward"] = reward
	}
	return result, nil
}

func (s ethService) GetCode(_ common.Address, _ string) string {
	return "0x"
}

func (s ethService) GetStorageAt(_ common.Address, _ string, _ string) string {
	return "0x0000000000000000000000000000000000000000000000000000000000000000"
}

func (s ethService) Call(_ map[string]any, _ *string) string {
	return "0x"
}

func (s ethService) EstimateGas(_ map[string]any) string {
	return hexQuantityUint64(defaultEstimateGas)
}

func (s ethService) SendRawTransaction(raw hexutil.Bytes) (common.Hash, error) {
	translation, err := s.server.translateEthereumValueTransfer(raw)
	if err != nil {
		s.server.logger.Error("eth_sendRawTransaction translation rejected transaction", "error", err)
		return common.Hash{}, err
	}

	s.server.logger.Info("eth_sendRawTransaction translation built candidate Celestia transaction", "ethereum_tx_hash", translation.EthereumTxHash)
	if s.server.txBroadcaster == nil {
		return common.Hash{}, errSendRawTransactionBroadcastDisabled
	}

	resp, err := s.server.txBroadcaster(translation.TxRawBytes)
	if err != nil {
		s.server.logger.Error("eth_sendRawTransaction failed to broadcast translated Celestia transaction", "ethereum_tx_hash", translation.EthereumTxHash, "error", err)
		return common.Hash{}, err
	}
	if resp == nil {
		return common.Hash{}, errors.New("broadcast translated Celestia transaction returned nil response")
	}
	if resp.Code != 0 {
		err := fmt.Errorf("broadcast translated Celestia transaction failed with code %d codespace %s: %s", resp.Code, resp.Codespace, resp.RawLog)
		s.server.logger.Error("eth_sendRawTransaction broadcast rejected", "ethereum_tx_hash", translation.EthereumTxHash, "error", err)
		return common.Hash{}, err
	}

	s.server.logger.Info("eth_sendRawTransaction broadcast translated Celestia transaction", "ethereum_tx_hash", translation.EthereumTxHash, "celestia_tx_hash", resp.TxHash)
	return common.HexToHash(translation.EthereumTxHash), nil
}

func (s ethService) ethereumChainID() (uint64, error) {
	return s.server.ethereumChainID()
}

func (s netService) ethereumChainID() (uint64, error) {
	return s.server.ethereumChainID()
}

func (s ethService) contextProvider() (sdk.Context, error) {
	return s.server.contextProvider()
}

func (s ethService) gasPriceProvider() (float64, error) {
	return s.server.gasPriceProvider()
}

func (s ethService) balance(ethAddr common.Address) (*big.Int, error) {
	return s.server.balance(ethAddr)
}

func (s ethService) transactionCount(ethAddr common.Address) (uint64, error) {
	return s.server.transactionCount(ethAddr)
}

func (s ethService) block(ctx sdk.Context) map[string]any {
	return s.server.block(ctx)
}

func (s web3Service) clientVersion() string {
	return s.server.clientVersion
}

func (s *Server) ethereumChainID() (uint64, error) {
	return EthereumChainIDForCelestia(s.chainIDProvider())
}

func (s *Server) balance(ethAddr common.Address) (*big.Int, error) {
	ctx, err := s.contextProvider()
	if err != nil {
		return nil, err
	}

	celAddr, found := s.identityKeeper.Resolve(ctx, ethAddr.Bytes())
	if !found {
		return big.NewInt(0), nil
	}

	balance := s.bankKeeper.GetBalance(ctx, celAddr, appconsts.BondDenom)
	return toEVMUnits(balance.Amount.BigInt()), nil
}

func (s *Server) transactionCount(ethAddr common.Address) (uint64, error) {
	ctx, err := s.contextProvider()
	if err != nil {
		return 0, err
	}

	celAddr, found := s.identityKeeper.Resolve(ctx, ethAddr.Bytes())
	if !found {
		return 0, nil
	}

	acc := s.accountKeeper.GetAccount(ctx, celAddr)
	if acc == nil {
		return 0, nil
	}
	return acc.GetSequence(), nil
}

func (s *Server) translateEthereumValueTransfer(raw []byte) (ethereumTxTranslation, error) {
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("decode ethereum transaction: %w", err)
	}
	if tx.Type() != types.DynamicFeeTxType {
		return ethereumTxTranslation{}, fmt.Errorf("unsupported ethereum transaction type %d", tx.Type())
	}
	to := tx.To()
	if to == nil {
		return ethereumTxTranslation{}, errors.New("contract creation is not supported")
	}
	if len(tx.Data()) != 0 {
		return ethereumTxTranslation{}, errors.New("non-empty data is not supported for native value transfer")
	}

	chainID, err := s.ethereumChainID()
	if err != nil {
		return ethereumTxTranslation{}, err
	}
	if tx.ChainId().Cmp(new(big.Int).SetUint64(chainID)) != 0 {
		return ethereumTxTranslation{}, fmt.Errorf("ethereum chain ID %s does not match configured chain ID %d", tx.ChainId(), chainID)
	}

	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, &tx)
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("recover ethereum sender: %w", err)
	}

	ctx, err := s.contextProvider()
	if err != nil {
		return ethereumTxTranslation{}, err
	}
	fromCelestia, found := s.identityKeeper.Resolve(ctx, from.Bytes())
	if !found {
		return ethereumTxTranslation{}, fmt.Errorf("unresolved ethereum sender %s", from.Hex())
	}
	toCelestia, found := s.identityKeeper.Resolve(ctx, to.Bytes())
	if !found {
		return ethereumTxTranslation{}, fmt.Errorf("unresolved ethereum recipient %s", to.Hex())
	}

	account := s.accountKeeper.GetAccount(ctx, fromCelestia)
	if account == nil {
		return ethereumTxTranslation{}, fmt.Errorf("canonical sender account %s not found", fromCelestia.String())
	}
	if tx.Nonce() != account.GetSequence() {
		return ethereumTxTranslation{}, fmt.Errorf("ethereum nonce %d does not match cosmos sequence %d", tx.Nonce(), account.GetSequence())
	}

	amount, err := fromEVMUnits(tx.Value())
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("convert ethereum value to native amount: %w", err)
	}
	if !amount.IsPositive() {
		return ethereumTxTranslation{}, errors.New("native transfer amount must be positive")
	}

	feeAmount, err := ethereumFeeToNative(tx.GasFeeCap(), tx.Gas())
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("convert ethereum fee cap to native fee amount: %w", err)
	}

	msg := banktypes.NewMsgSend(
		fromCelestia,
		toCelestia,
		sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amount)),
	)
	messages, err := txv1beta1.SetMsgs([]sdk.Msg{msg})
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("pack candidate message: %w", err)
	}
	ext, err := txethereum.NewExtensionOptions(txethereum.SchemaVersion, chainID, raw)
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("build Ethereum transaction extension option: %w", err)
	}
	bodyBytes, err := (&txv1beta1.TxBody{
		Messages:         messages,
		ExtensionOptions: []*codectypes.Any{ext},
	}).Marshal()
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("marshal candidate tx body: %w", err)
	}
	signature, err := txethereum.SignatureFromTx(&tx)
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("extract Ethereum transaction signature: %w", err)
	}
	authInfoBytes, err := (&txv1beta1.AuthInfo{
		SignerInfos: []*txv1beta1.SignerInfo{
			{
				ModeInfo: &txv1beta1.ModeInfo{
					Sum: &txv1beta1.ModeInfo_Single_{
						Single: &txv1beta1.ModeInfo_Single{
							Mode: txethereum.SignMode,
						},
					},
				},
				Sequence: tx.Nonce(),
			},
		},
		Fee: &txv1beta1.Fee{
			Amount:   sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, feeAmount)),
			GasLimit: tx.Gas(),
		},
	}).Marshal()
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("marshal candidate auth info: %w", err)
	}
	txRawBytes, err := (&txv1beta1.TxRaw{
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
		Signatures:    [][]byte{signature},
	}).Marshal()
	if err != nil {
		return ethereumTxTranslation{}, fmt.Errorf("marshal candidate tx raw: %w", err)
	}

	bodyHash := sha256.Sum256(bodyBytes)
	authInfoHash := sha256.Sum256(authInfoBytes)
	return ethereumTxTranslation{
		EthereumTxHash:      tx.Hash().Hex(),
		FromEthereumAddress: from.Hex(),
		ToEthereumAddress:   to.Hex(),
		FromCelestiaAddress: fromCelestia.String(),
		ToCelestiaAddress:   toCelestia.String(),
		Amount:              sdk.NewCoin(appconsts.BondDenom, amount).String(),
		FeeAmount:           sdk.NewCoin(appconsts.BondDenom, feeAmount).String(),
		GasLimit:            tx.Gas(),
		Sequence:            tx.Nonce(),
		BodyBytesHash:       "0x" + hex.EncodeToString(bodyHash[:]),
		AuthInfoBytesHash:   "0x" + hex.EncodeToString(authInfoHash[:]),
		TxRawBytesLen:       len(txRawBytes),
		TxRawBytes:          txRawBytes,
	}, nil
}

func (s *Server) block(ctx sdk.Context) map[string]any {
	header := ctx.BlockHeader()
	timestamp := uint64(0)
	if !ctx.BlockTime().IsZero() {
		timestamp = uint64(ctx.BlockTime().Unix())
	}

	return map[string]any{
		"number":           hexQuantityInt64(ctx.BlockHeight()),
		"hash":             hashQuantity(header.AppHash),
		"parentHash":       hashQuantity(header.LastBlockId.Hash),
		"nonce":            "0x0000000000000000",
		"sha3Uncles":       emptyUncleHash,
		"logsBloom":        zeroBloomQuantity(),
		"transactionsRoot": emptyRootHash,
		"stateRoot":        hashQuantity(header.AppHash),
		"receiptsRoot":     emptyRootHash,
		"miner":            common.Address{}.Hex(),
		"difficulty":       "0x0",
		"totalDifficulty":  "0x0",
		"extraData":        "0x",
		"size":             "0x0",
		"gasLimit":         "0x0",
		"gasUsed":          "0x0",
		"timestamp":        hexQuantityUint64(timestamp),
		"transactions":     []any{},
		"uncles":           []string{},
		"baseFeePerGas":    "0x0",
	}
}

func hashQuantity(value []byte) string {
	if len(value) == 0 {
		return common.Hash{}.Hex()
	}
	return common.BytesToHash(value).Hex()
}

func zeroBloomQuantity() string {
	return "0x" + fmt.Sprintf("%0512x", 0)
}

func hexQuantityUint64(value uint64) string {
	return fmt.Sprintf("0x%x", value)
}

func hexQuantityInt64(value int64) string {
	if value <= 0 {
		return "0x0"
	}
	return fmt.Sprintf("0x%x", value)
}

func hexQuantityBigInt(value *big.Int) string {
	if value == nil || value.Sign() <= 0 {
		return "0x0"
	}
	return "0x" + value.Text(16)
}

func toEVMUnits(nativeAmount *big.Int) *big.Int {
	if nativeAmount == nil || nativeAmount.Sign() == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Mul(nativeAmount, evmUnitMultiplier)
}

func fromEVMUnits(evmAmount *big.Int) (sdkmath.Int, error) {
	if evmAmount == nil || evmAmount.Sign() < 0 {
		return sdkmath.Int{}, errors.New("amount must be non-negative")
	}
	nativeAmount, remainder := new(big.Int).QuoRem(evmAmount, evmUnitMultiplier, new(big.Int))
	if remainder.Sign() != 0 {
		return sdkmath.Int{}, fmt.Errorf("amount %s is not divisible by %s", evmAmount, evmUnitMultiplier)
	}
	return sdkmath.NewIntFromBigInt(nativeAmount), nil
}

func ethereumFeeToNative(gasFeeCap *big.Int, gasLimit uint64) (sdkmath.Int, error) {
	if gasFeeCap == nil || gasFeeCap.Sign() < 0 {
		return sdkmath.Int{}, errors.New("gas fee cap must be non-negative")
	}
	totalFee := new(big.Int).Mul(gasFeeCap, new(big.Int).SetUint64(gasLimit))
	return fromEVMUnits(totalFee)
}

func ceilGasPrice(value float64) uint64 {
	if value <= 0 {
		return 0
	}
	if value > float64(maxUint64) {
		return maxUint64
	}
	return uint64(math.Ceil(value))
}

type ethereumTxTranslation struct {
	EthereumTxHash      string
	FromEthereumAddress string
	ToEthereumAddress   string
	FromCelestiaAddress string
	ToCelestiaAddress   string
	Amount              string
	FeeAmount           string
	GasLimit            uint64
	Sequence            uint64
	BodyBytesHash       string
	AuthInfoBytesHash   string
	TxRawBytesLen       int
	TxRawBytes          []byte
}
