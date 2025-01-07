# ADR 003: QGB Data Commitments

## Status

Implemented

## Context

To accommodate the requirements of the [Quantum Gravity Bridge](https://github.com/celestiaorg/quantum-gravity-bridge/blob/76efeca0be1a17d32ef633c0fdbd3c8f5e4cc53f/src/QuantumGravityBridge.sol), We will need to add support for `DataCommitment`s messages, i.e. commitments generated over a set of blocks to attest their existence.

## Decision

Add the `DataCommitmentConfirm` type of messages in order to attest that a set of blocks has been finalized.

PS: The `ValsetConfirm` have been updated in `adr-005-qgb-reduce-state-usage`. Please take a look at it to know how we will be handling the confirms.

## Detailed Design

To accommodate the QGB, validators need a way to submit signatures for data commitments so that relayers can easily find them and submit them to the bridged chain. To do this, we will introduce `MsgDataCommitmentConfirm` messages. This latter will be persisted for the sole reason of slashing. If not for that, a P2P network would do the job.

Data commitment messages attest that a certain set of blocks have been committed to in the Celestia chain. These commitments are used to update the data commitment checkpoint defined in the Ethereum smart contract of the QGB.

Thus, they will contain the commitments along with the signatures and will be used to check if an attestation has been signed by 2/3+ of the network validators and can be committed to the bridge contract.

### MsgDataCommitmentConfirm

`MsgDataCommitmentConfirm` describe a data commitment for a set of blocks signed by an orchestrator.

```protobuf
message MsgDataCommitmentConfirm {
  // Signature over the commitment, the range of blocks, the validator address
  // and the Ethereum address.
  string signature = 1;
  // Orchestrator account address who will be signing the message.
  string validator_address = 2;
  // Hex `0x` encoded Ethereum public key that will be used by this validator on
  // Ethereum.
  string eth_address = 3;
  // Merkle root over a merkle tree containing the data roots of a set of
  // blocks.
  string commitment = 4;
  // First block defining the ordered set of blocks used to create the
  // commitment.
  int64 begin_block = 5;
  // Last block defining the ordered set of blocks used to create the
  // commitment.
  int64 end_block = 6;
}
```

#### Data commitment message processing

When handling a `MsgDataCommitmentConfirm`, we go for the following:

#### Verify the signature

We start off by verifying if the signature is well-formed, and return an error if not:

```go
sigBytes, err := hex.DecodeString(msg.Signature)
if err != nil {
    return nil, sdkerrors.Wrap(types.ErrInvalid, "signature decoding")
}
```

This is done first as the whole concept revolves around signing commitments. Thus, if a signature is malformed, there is no need to continue.

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

#### Verify the Ethereum address and check signature

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

#### Set the data commitment to confirm and send the event

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
