# ADR 002: Overview

## Changelog

- {date}: {changelog}

## Context
To accommodate the requirements of the [Quantum Gravity Bridge](https://github.com/celestiaorg/quantum-gravity-bridge/blob/master/ethereum/solidity/src/QuantumGravityBridge.sol),
We will need to add support for `ValSet`s, i.e. Validator Sets, which reflect the current state of the bridge validators.

## Decision
Add the `ValSet` and `ValSetConfirm` type of messages in order to track the state of the validator set.

## Detailed Design
// Copied from https://github.com/Gravity-Bridge/Gravity-Bridge/edit/main/spec/valset-creation-spec.md
In QGB, when we talk about a `valset` we mean a `validator set update` which is a series of ethereum addresses with attached normalized powers used
to represent the Cosmos validator set in the Gravity Ethereum contract. Since the Cosmos validator set can and will change frequently.

Validator set creation is a critical part of the QGB system. The goal is to produce and sign enough validator sets that no matter which one is in
the Ethereum contract there is an unbroken chain of correctly signed state updates (greater than 66% of the previous voting power) to sync the Ethereum
contract with the current Cosmos validator set.

The key to understanding valset creation is to understand that it is _absolutely impossible_ for either side be fully synced with the other. The Cosmos chain has
finality, but produces blocks so much faster than Ethereum that the validator set could change completely an arbitrary number of times between Ethereum blocks.
In the other direction Ethereum does not have finality, so there is a significant block delay before the Cosmos chain can know what occurred on Ethereum.
It's because of these fundamental restrictions that we focus on continuity of produced validator sets rather than determining what the 'last state on Ethereum' is.
// FIXME What if we have two chains with the same finality property and same block time. According to this paragraph, we wouldn't need a valset. But, how are we going to decide on who will sign?

We generate a validator set update for a given percentage of power change. Note that power change is computed in a normalized fashion, whereas Cosmos power exits
relative to some changing total power value. So for example if the total power on Cosmos increased 10% due purely to inflation, the power in the Gravity bridge
contract would not change at all, as all validators would inflate equally barring slashing or some other event.
// FIXME this also doesn't make much sense to me.

Currently, a 5% power change threshold has been selected somewhat arbitrarily. A survey of power on the hub says that a power change of less than 1% a week is common,
but as a general number for all Cosmos zones it may be a little too conservative. This should probably be broken out into a parameter and changed
as required by the active validator set.

The main consideration when setting this parameter is that your security is reduced, instead of requiring 66% of validators to execute a message on Ethereum,
you may need up to 66% + power change percentage of the current active validator set.
Of course the ideal case is to generate validator sets on every power change of any kind, but this is infeasible both from a signature submission
perspective (submitting hundreds of messages per block to handle signatures is infeasible) but also infeasible from a cost standpoint.

### When are validator sets created

1. If there are no valset requests, create a new one
2. If there is at least one validator who started unbonding in current block. (we persist last unbonded block height in `hooks.go`)
   This will make sure the unbonding validator has to provide an attestation to a new Valset
   that excludes him before he completely Unbonds. Otherwise, he will be slashed.
3. If power change between validators of CurrentValset and latest valset request is > 5%

### Message types
We added the following messages types:

#### Bridge Validator
The `BridgeValidator` represents a validator's ETH address and its power.
```protobuf
message BridgeValidator {
  uint64 power            = 1;
  string ethereum_address = 2;
}
```
It contains:
- `power`: the voting power of the validator.
- `ethereum_address`: the Ethereum address that will be used by the validator to sign messages.

#### ValSet
`Valset` is the Ethereum Bridge Multsig Set, each qgb validator also maintains an ETH key
to sign messages, these are used to check signatures on ETH because of the significant gas savings.
```protobuf
message Valset {
  uint64                   nonce   = 1;
  repeated BridgeValidator members = 2 [(gogoproto.nullable) = false];
  uint64                   height  = 3;
}
```
It contains:
- `nonce`: a unique number referencing the `ValSet`.
- `BridgeValidator`: a list of [BridgeValidator](#Bridge-Validator) containing the current validator set.
- `height`: the current chain height.

#### MsgSetOrchestratorAddress
`MsgSetOrchestratorAddress` allows validators to delegate their voting responsibilities
to a given key. This key is then used as an optional authentication method for signing
oracle claims.
```protobuf
message MsgSetOrchestratorAddress {
   string validator    = 1;
   string orchestrator = 2;
   string eth_address  = 3;
}
```
It contains:
- `validator`: a `celesvaloper1` address referencing the validator in the current `ValSet`.
- `orchestrator`: a `celes1` account address referencing the key that is being delegated to.
- `eth_address`: the hex `0x` encoded Ethereum public key that will be used by this validator on Ethereum.

#### ValSetConfirm
`MsgValsetConfirm` is the message sent by the validators when they wish to submit their signatures
over the validator set at a given block height. A validator must first call `MsgSetEthAddress` to
set their Ethereum address to be used for signing.
Then, someone (anyone) must make a `ValsetRequest`, the request is essentially a messaging mechanism
to determine which block all validators  should submit signatures over. Finally, validators sign
the `validator set`, `powers`, and `Ethereum addresses` of the entire validator set at the height
of a `Valset` and submit that signature with this message.

If a sufficient number of validators (66% of voting power):
- have set Ethereum addresses and,
- submit `ValsetConfirm` messages with their signatures,
it is then possible for anyone to view these signatures in the chain store and submit them 
to Ethereum to update the validator set.
```protobuf
message MsgValsetConfirm {
  uint64 nonce        = 1;
  string orchestrator = 2;
  string eth_address  = 3;
  string signature    = 4;
}
```
It contains:
- `nonce`: a unique number referencing the `ValSet`.
- `orchestrator`: the orchestrator `celes1` account address.
- `eth_address`: the Ethereum address, associated to the orchestrator, used to sign the `ValSet` message.
- `signature`: the `ValSet` message signature.

### ValSetConfirm Processing
Upon receiving a `MsgValSetConfirm`, we go for the following:

#### ValSet check
We start off by checking if the `ValSet` referenced by the provided `nonce` exists. If so, we 
get it. If not, we return an error:
```go
	valset := k.GetValset(ctx, msg.Nonce)
	if valset == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "couldn't find valset")
	}
```

#### Check the address and signature
Next, we check the orchestrator address: 
```go
orchaddr, err := sdk.AccAddressFromBech32(msg.Orchestrator)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "acc address invalid")
	}
```

Then, we verify if the signature is well-formed, and it is signed using a private key whose address
is the one sent in the request:
```go
    err = k.confirmHandlerCommon(ctx, msg.EthAddress, msg.Orchestrator, msg.Signature)
	if err != nil {
		return nil, err
	}
    // persist signature
    if k.GetValsetConfirm(ctx, msg.Nonce, orchaddr) != nil {
        return nil, sdkerrors.Wrap(types.ErrDuplicate, "signature duplicate")
    }
```

The `confirmHandlerCommon` is an internal function that provides common code for processing signatures:
```go
func (k msgServer) confirmHandlerCommon(ctx sdk.Context, ethAddress string, orchestrator string, signature string) error {
	_, err := hex.DecodeString(signature)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}

	submittedEthAddress, err := types.NewEthAddress(ethAddress)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}

	orchaddr, err := sdk.AccAddressFromBech32(orchestrator)
	if err != nil {
		return sdkerrors.Wrap(types.ErrInvalid, "acc address invalid")
	}
	validator, found := k.GetOrchestratorValidator(ctx, orchaddr)
	if !found {
		return sdkerrors.Wrap(types.ErrUnknown, "validator")
	}
	if err := sdk.VerifyAddressFormat(validator.GetOperator()); err != nil {
		return sdkerrors.Wrapf(err, "discovered invalid validator address for orchestrator %v", orchaddr)
	}

	ethAddressFromStore, found := k.GetEthAddressByValidator(ctx, validator.GetOperator())
	if !found {
		return sdkerrors.Wrap(types.ErrEmpty, "no eth address set for validator")
	}

	if *ethAddressFromStore != *submittedEthAddress {
		return sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}
	return nil
}
```
And, then check if the signature is a duplicate, i.e. whether another `ValSetConfirm` reflecting the same 
truth has already been commited to.

#### Persist the ValSet confirm and emit an event
Lastly, we persist the `ValSetConfirm` message and broadcast an event:
```go
	key := k.SetValsetConfirm(ctx, *msg)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, msg.Type()),
			sdk.NewAttribute(types.AttributeKeyValsetConfirmKey, string(key)),
		),
	)
```

### SetOrchestratorAddress Processing
Upon receiving a `MsgSetOrchestratorAddress`, we go for the following:

#### Basic validation
We start off by validating the parameters:
```go
// ensure that this passes validation, checks the key validity
	err := msg.ValidateBasic()
	if err != nil {
		return nil, sdkerrors.Wrap(err, "Key not valid")
	}

	ctx := sdk.UnwrapSDKContext(c)

	// check the following, all should be validated in validate basic
	val, e1 := sdk.ValAddressFromBech32(msg.Validator)
	orch, e2 := sdk.AccAddressFromBech32(msg.Orchestrator)
	addr, e3 := types.NewEthAddress(msg.EthAddress)
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, sdkerrors.Wrap(err, "Key not valid")
	}

	// check that the validator does not have an existing key
	_, foundExistingOrchestratorKey := k.GetOrchestratorValidator(ctx, orch)
	_, foundExistingEthAddress := k.GetEthAddressByValidator(ctx, val)

	// ensure that the validator exists
	if foundExistingOrchestratorKey || foundExistingEthAddress {
		return nil, sdkerrors.Wrap(types.ErrResetDelegateKeys, val.String())
	}
```

Then, verify that neither keys is a duplicate:
```go
	// check that neither key is a duplicate
	delegateKeys := k.GetDelegateKeys(ctx)
	for i := range delegateKeys {
		if delegateKeys[i].EthAddress == addr.GetAddress() {
			return nil, sdkerrors.Wrap(err, "Duplicate Ethereum Key")
		}
		if delegateKeys[i].Orchestrator == orch.String() {
			return nil, sdkerrors.Wrap(err, "Duplicate Orchestrator Key")
		}
	}
```

#### Persist the Orchestrator and Ethereum address and emit an event
Lastly, we persist the orchestrator validator address:
```go
    k.SetOrchestratorValidator(ctx, val, orch)
```

Then, we set the corresponding Ethereum address:
```go
   k.SetEthAddressForValidator(ctx, val, *addr)
```

And finally, emit an event:
```go
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, msg.Type()),
			sdk.NewAttribute(types.AttributeKeySetOperatorAddr, orch.String()),
		),
	)
```

## Status
Accepted

## References

- {reference link}
