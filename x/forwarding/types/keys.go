package types

import (
	fmt "fmt"
	"math/big"

	"cosmossdk.io/collections"
	"github.com/ethereum/go-ethereum/accounts/abi"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/gogoproto/proto"
)

const (
	// ModuleName defines the module name.
	ModuleName = "forwarding"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// RouterKey is the message route for slashing.
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key.
	QuerierRoute = ModuleName
)

const (
	HyperlaneModuleID = 255
)

var (
	RoutersKeyPrefix       = collections.NewPrefix(0)
	RemoteRoutersKeyPrefix = collections.NewPrefix(1)
)

func DeriveForwardAddress(derivationKeys ...[]byte) sdk.AccAddress {
	return address.Module(ModuleName, derivationKeys...)
}

type InterchainAccountsPayload struct {
	MessageType uint8
	Owner       [32]byte
	Ism         [32]byte
	Salt        [32]byte
	Calls       []codectypes.Any
}

type Call struct {
	To    [32]byte `abi:"to"`
	Value *big.Int `abi:"value"`
	Data  []byte   `abi:"data"`
}

const (
	MessageTypeCalls = uint8(0)
)

var interchainAccountCallsArgs = func() abi.Arguments {
	callType, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "bytes32"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})
	if err != nil {
		panic(err)
	}
	return abi.Arguments{{Type: callType}}
}()

/**
 * Format of CALLS message:
 * [   0:   1] MessageType.CALLS (uint8)
 * [   1:  33] ICA owner (bytes32)
 * [  33:  65] ICA ISM (bytes32)
 * [  65:  97] User Salt (bytes32)
 * [  97:????] Calls (CallLib.Call[]), abi encoded
 *
 * Format of COMMITMENT message (Unsupported):
 * [   0:   1] MessageType.COMMITMENT (uint8)
 * [   1:  33] ICA owner (bytes32)
 * [  33:  65] ICA ISM (bytes32)
 * [  65:  97] User Salt (bytes32)
 * [  97: 129] Commitment (bytes32)
 */
func ParseInterchainAccountsPayload(data []byte) (InterchainAccountsPayload, error) {
	if len(data) < 97 {
		return InterchainAccountsPayload{}, fmt.Errorf("unexpected payload data length, too short")
	}

	if data[0] != MessageTypeCalls {
		return InterchainAccountsPayload{}, fmt.Errorf("unsupported interchain accounts message type %d", data[0])
	}

	var owner [32]byte
	copy(owner[:], data[1:33])
	var ism [32]byte
	copy(ism[:], data[33:65])
	var salt [32]byte
	copy(salt[:], data[65:97])

	values, err := interchainAccountCallsArgs.Unpack(data[97:])
	if err != nil {
		return InterchainAccountsPayload{}, err
	}

	var calls []Call
	if err := interchainAccountCallsArgs.Copy(&calls, values); err != nil {
		return InterchainAccountsPayload{}, err
	}

	decodedCalls := make([]codectypes.Any, len(calls))
	for i, call := range calls {
		var any codectypes.Any
		if err := proto.Unmarshal(call.Data, &any); err != nil {
			return InterchainAccountsPayload{}, err
		}
		decodedCalls[i] = any
	}

	return InterchainAccountsPayload{
		MessageType: MessageTypeCalls,
		Owner:       owner,
		Ism:         ism,
		Salt:        salt,
		Calls:       decodedCalls,
	}, nil
}
