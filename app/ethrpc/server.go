package ethrpc

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/http"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/mux"
)

const maxUint64 = ^uint64(0)

const (
	defaultEstimateGas = 21000
	nativeDecimals     = 6
	evmDecimals        = 18
	emptyUncleHash     = "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"
	emptyRootHash      = "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
)

var evmUnitMultiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(evmDecimals-nativeDecimals), nil)

// ContextProvider returns the latest committed query context.
type ContextProvider func() (sdk.Context, error)

// ChainIDProvider returns the active Celestia chain ID.
type ChainIDProvider func() string

// GasPriceProvider returns the native minimum gas price in utia per gas.
type GasPriceProvider func() (float64, error)

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

// Server handles read-only Ethereum JSON-RPC wallet display methods.
type Server struct {
	contextProvider  ContextProvider
	chainIDProvider  ChainIDProvider
	gasPriceProvider GasPriceProvider
	identityKeeper   EthIdentityKeeper
	accountKeeper    AccountKeeper
	bankKeeper       BankKeeper
	clientVersion    string
	rpc              *gethrpc.Server
}

// Config defines dependencies for the Ethereum JSON-RPC server.
type Config struct {
	ContextProvider  ContextProvider
	ChainIDProvider  ChainIDProvider
	GasPriceProvider GasPriceProvider
	IdentityKeeper   EthIdentityKeeper
	AccountKeeper    AccountKeeper
	BankKeeper       BankKeeper
	ClientVersion    string
}

// NewServer creates a read-only Ethereum JSON-RPC server.
func NewServer(cfg Config) *Server {
	server := &Server{
		contextProvider:  cfg.ContextProvider,
		chainIDProvider:  cfg.ChainIDProvider,
		gasPriceProvider: cfg.GasPriceProvider,
		identityKeeper:   cfg.IdentityKeeper,
		accountKeeper:    cfg.AccountKeeper,
		bankKeeper:       cfg.BankKeeper,
		clientVersion:    cfg.ClientVersion,
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

func ceilGasPrice(value float64) uint64 {
	if value <= 0 {
		return 0
	}
	if value > float64(maxUint64) {
		return maxUint64
	}
	return uint64(math.Ceil(value))
}
