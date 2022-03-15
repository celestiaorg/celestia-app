# ADR 002: Overview

## Changelog

- {date}: {changelog}

## Context
To accommodate the requirements of the [Quantum Gravity Bridge](https://github.com/celestiaorg/quantum-gravity-bridge/blob/master/ethereum/solidity/src/QuantumGravityBridge.sol),
We will need to add support for `DataCommitment`s messages, i.e. commitments generated over a set of blocks to attest their existence.

## Decision
Add the `DataCommitmentConfirm` type of messages in order to attest that a set of blocks has been finalized.

## Detailed Design
Data commitment messages attest that a certain set of blocks have been commited to in the Celestia chain. These commitments are used to update the data commitment checkpoint
defined in the Ethereum smart contract of the QGB.

#### MsgDataCommitmentConfirm
`MsgDataCommitmentConfirm` describe a data commitment for a set of blocks signed by an orchestrator.
```protobuf
message MsgDataCommitmentConfirm {
  string signature = 1;
  string validator_address = 2;
  string eth_address = 3;
  string commitment = 4;
  int64 begin_block = 5;
  int64 end_block = 6;
}
```
It contains:
- `signature`: the signature over the commitment, the range of blocks, the validator address and the Ethereum address.
- `validator_address`: the orchestrator account address who will be signing the message.
- `eth_address`: the hex `0x` encoded Ethereum public key that will be used by this validator on Ethereum.
- `commitment`: the merkle root over a merkle tree containing the data roots of a set of blocks.
- `begin_block`: the first block defining the ordered set of blocks used to create the commitment.
- `end_block`: the last block defining the ordered set of blocks used to create the commitment.

### Data commitment message processing
When handling a `MsgDataCommitmentConfirm`, we go for the following:

#### Verify the signature
We start off by verifying if the signature is well-formed, and return an error if not:
```go
	sigBytes, err := hex.DecodeString(msg.Signature)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
	}
```
This is done first as the whole concept revolves around signing commitments. Thus, if a signature is mal-formed, there is no need to continue.

#### Verify addresses
Then, we verify the provided address and check if an orchestrator exists for the provided address:
```go
	validatorAddress, err := sdk.AccAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "validator address invalid")
	}
	validator, found := k.GetOrchestratorValidator(ctx, validatorAddress)
	if !found {
		return nil, sdkerrors.Wrap(types.ErrUnknown, "validator")
	}
	if err := sdk.VerifyAddressFormat(validator.GetOperator()); err != nil {
		return nil, sdkerrors.Wrapf(err, "discovered invalid validator address for validator %v", validatorAddress)
	}
```

#### Verify Ethereum address and check signature
Next, we verify the Ethereum address and check if it was used to create the signature:
```go
ethAddress, err := types.NewEthAddress(msg.EthAddress)
	if err != nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "invalid eth address")
	}
	err = types.ValidateEthereumSignature([]byte(msg.Commitment), sigBytes, *ethAddress)
	if err != nil {
		return nil,
			sdkerrors.Wrap(
				types.ErrInvalid,
				fmt.Sprintf(
					"signature verification failed expected sig by %s with checkpoint %s found %s",
					ethAddress,
					msg.Commitment,
					msg.Signature,
				),
			)
	}
	ethAddressFromStore, found := k.GetEthAddressByValidator(ctx, validator.GetOperator())
	if !found {
		return nil, sdkerrors.Wrap(types.ErrEmpty, "no eth address set for validator")
	}
	if *ethAddressFromStore != *ethAddress {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "submitted eth address does not match delegate eth address")
	}
```

#### Set the data commitment confirm and send event
After checking that the message is valid, the addresses are correct and the signature is legit, we proceed to persist this data commitment confirm message:
```go
    k.SetDataCommitmentConfirm(ctx, *msg)
```
Then, we continue to broadcast an event containing the message:
```go
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, msg.Type()),
			sdk.NewAttribute(types.AttributeKeyDataCommitmentConfirmKey, msg.String()),
		),
	)
```

## Status
Accepted

## References

- {reference link}
