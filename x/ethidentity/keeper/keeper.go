package keeper

import (
	"bytes"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/x/ethidentity/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const ethAddressLength = 20

// Keeper maintains the consensus-state index from Ethereum addresses to
// canonical Celestia account addresses.
type Keeper struct {
	storeKey storetypes.StoreKey
}

// NewKeeper creates an ethidentity keeper.
func NewKeeper(storeKey storetypes.StoreKey) Keeper {
	return Keeper{storeKey: storeKey}
}

// IndexPubKey records the same-key Ethereum and Celestia identities derived
// from pubKey. Non-secp256k1 public keys are ignored.
func (k Keeper) IndexPubKey(ctx sdk.Context, pubKey cryptotypes.PubKey) error {
	secpPubKey, ok := pubKey.(*secp256k1.PubKey)
	if !ok {
		return nil
	}

	ethAddr, err := EthereumAddressFromPubKey(secpPubKey)
	if err != nil {
		return err
	}

	return k.indexMapping(ctx, ethAddr, sdk.AccAddress(secpPubKey.Address()))
}

func (k Keeper) indexMapping(ctx sdk.Context, ethAddr []byte, celAddr sdk.AccAddress) error {
	if len(ethAddr) != ethAddressLength {
		return fmt.Errorf("invalid Ethereum address length %d", len(ethAddr))
	}
	if len(celAddr) == 0 {
		return fmt.Errorf("missing Celestia address")
	}

	store := ctx.KVStore(k.storeKey)
	key := ethAddressKey(ethAddr)
	existing := store.Get(key)
	if existing == nil {
		store.Set(key, celAddr)
		return nil
	}
	if !bytes.Equal(existing, celAddr) {
		return fmt.Errorf("Ethereum address is already indexed to a different Celestia address")
	}
	return nil
}

// Resolve returns the Celestia address indexed for ethAddr, if one exists.
func (k Keeper) Resolve(ctx sdk.Context, ethAddr []byte) (sdk.AccAddress, bool) {
	if len(ethAddr) != ethAddressLength {
		return nil, false
	}

	value := ctx.KVStore(k.storeKey).Get(ethAddressKey(ethAddr))
	if value == nil {
		return nil, false
	}
	return sdk.AccAddress(value), true
}

// InitGenesis initializes the keeper state from genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, genesis types.GenesisState) error {
	for _, mapping := range genesis.Mappings {
		ethAddr, celAddr, err := mappingAddresses(mapping)
		if err != nil {
			return err
		}
		if err := k.indexMapping(ctx, ethAddr, celAddr); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis exports all indexed identities.
func (k Keeper) ExportGenesis(ctx sdk.Context) types.GenesisState {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.EthAddressIndexPrefix)
	defer iterator.Close()

	genesis := types.GenesisState{}
	for ; iterator.Valid(); iterator.Next() {
		ethAddr := iterator.Key()[len(types.EthAddressIndexPrefix):]
		celAddr := sdk.AccAddress(iterator.Value())
		genesis.Mappings = append(genesis.Mappings, types.Mapping{
			EthereumAddress: common.BytesToAddress(ethAddr).Hex(),
			CelestiaAddress: celAddr.String(),
		})
	}
	return genesis
}

// EthereumAddressFromPubKey derives the Ethereum address for a compressed
// secp256k1 public key.
func EthereumAddressFromPubKey(pubKey *secp256k1.PubKey) ([]byte, error) {
	if pubKey == nil {
		return nil, fmt.Errorf("missing public key")
	}
	ecdsaPubKey, err := gethcrypto.DecompressPubkey(pubKey.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress secp256k1 public key: %w", err)
	}
	ethAddr := gethcrypto.PubkeyToAddress(*ecdsaPubKey)
	return ethAddr.Bytes(), nil
}

func ethAddressKey(ethAddr []byte) []byte {
	key := make([]byte, 0, len(types.EthAddressIndexPrefix)+len(ethAddr))
	key = append(key, types.EthAddressIndexPrefix...)
	key = append(key, ethAddr...)
	return key
}

func mappingAddresses(mapping types.Mapping) ([]byte, sdk.AccAddress, error) {
	if !common.IsHexAddress(mapping.EthereumAddress) {
		return nil, nil, fmt.Errorf("invalid Ethereum address %q", mapping.EthereumAddress)
	}
	celAddr, err := sdk.AccAddressFromBech32(mapping.CelestiaAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Celestia address %q: %w", mapping.CelestiaAddress, err)
	}
	return common.HexToAddress(mapping.EthereumAddress).Bytes(), celAddr, nil
}
