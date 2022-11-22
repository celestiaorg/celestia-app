# Quantum gravity bridge state machine 

This module contains the [Quantum gravity bridge](https://blog.celestia.org/celestiums/) state machine implementation.

## State machine

The QGB state machine handles the creation of the attestations requests:

https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/x/qgb/types/attestation.go#L15-L23

These latter is either data commitments:

```proto
// DataCommitment is the data commitment request message that will be signed  
// using orchestrators.  
// It does not contain a `commitment` field as this message will be created  
// inside the state machine and it doesn't make sense to ask tendermint for the  
// commitment there.  
message DataCommitment {  
  option (cosmos_proto.implements_interface) = "AttestationRequestI";  
  // Universal nonce defined under:  
  // https://github.com/celestiaorg/celestia-app/pull/464  uint64 nonce = 1;  
  // First block defining the ordered set of blocks used to create the  
  // commitment.  uint64 begin_block = 2;  
  // Last block defining the ordered set of blocks used to create the  
  // commitment.  uint64 end_block = 3;  
}
```

Or, valsets:

```proto
// Valset is the EVM Bridge Multsig Set, each qgb validator also  
// maintains an ETH key to sign messages, these are used to check signatures on  
// ETH because of the significant gas savings  
message Valset {  
  option (cosmos_proto.implements_interface) = "AttestationRequestI";  
  // Universal nonce defined under:  
  // https://github.com/celestiaorg/celestia-app/pull/464  uint64 nonce = 1;  
  // List of BridgeValidator containing the current validator set.  
  repeated BridgeValidator members = 2 [ (gogoproto.nullable) = false ];  
  // Current chain height  
  uint64 height = 3;  
}
```

During their creation, the state machine might panic due to an unexpected behavior or event. These panics will be discussed below.

### Conditions of generating a new attestation

#### New valset creation

A new valset is created in the following situations:

https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/x/qgb/abci.go#L74

- No valset exist in store, so a new valset will be created. This happens mostly after genesis, or after a hard fork.
- The current block height is the last unbonding height, i.e. when a validator is leaving the validator set. A new valset will need to be created to accommodate that change.
- A significant power difference happened since the last valset. This could happen if a validator has way more staking power or the opposite. The significant power difference threshold is defined by the constant `SignificantPowerDifferenceThreshold`, and it is set to 5% currently.

#### New data commitment creation

A new data commitment is created in the following situation:

https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/x/qgb/abci.go#L23

I.e. the current block height is not 0, and we're at a data commitment window height.

The data commitment window is defined as a governance parameter that can be changed. Currently, it is set to 400.

### Panics

During EndBlock step, the state machine generates new attestations if needed. During this generation, the state machine could panic.

#### Data commitment panics

During EndBlock, if the block height corresponds to a `DataCommitmentWindow`, it will generate a new data commitment, during which, the state machine can panic in the following case:

- An unexpected behavior happened while getting the current data commitment:

```golang
dataCommitment, err := k.GetCurrentDataCommitment(ctx)  
if err != nil {  
   panic(sdkerrors.Wrap(err, "coudln't get current data commitment"))  
}
```

#### Valset panics

Similar to data commitments, when checking if the state machine needs to generate a new valset, it might panic in the following cases:

- When checking that a previous valset has been emitted, but it is unable to get it:

```golang
if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) != 0 {  
   var err error  
   latestValset, err = k.GetLatestValset(ctx)  
   if err != nil {  
      panic(err)  
   }  
}
```

- When getting the current valset:

```golang
vs, err := k.GetCurrentValset(ctx)
if err != nil {  
   // this condition should only occur in the simulator  
   // ref : https://github.com/Gravity-Bridge/Gravity-Bridge/issues/35   if errors.Is(err, types.ErrNoValidators) {  
      ctx.Logger().Error("no bonded validators",  
         "cause", err.Error(),  
      )  
      return  
   }  
   panic(err)  
}
```

- When creating the internal validator struct, i.e. mapping the validators EVM addresses to their powers:

```golang
intLatestMembers, err := types.BridgeValidators(latestValset.Members).ToInternal()  
if err != nil {  
   panic(sdkerrors.Wrap(err, "invalid latest valset members"))  
}
```

#### Attestations panics

When storing a new attestation, which is either a data commitment or a valset, the state machine can panic for in the following cases:

- The attestation request created from the data commitment is a duplicate of an existing attestation:

```golang
key := []byte(types.GetAttestationKey(nonce))  
store := ctx.KVStore(k.storeKey)  
  
if store.Has(key) {  
   panic("trying to overwrite existing attestation request")  
}
```

- An error happened while marshalling the interface:

```golang
b, err := k.cdc.MarshalInterface(at)  
if err != nil {  
   panic(err)  
}
```

- The universal nonce was not incremented correctly by 1:

```golang
if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx)+1 != nonce {  
   panic("not incrementing latest attestation nonce correctly")  
}
```


## Useful links

The smart contract implementation is in [quantum-gravity-bridge](https://github.com/celestiaorg/quantum-gravity-bridge/).

The orchestrator and relayer implementations are in [orchestrator-relayer](https://github.com/celestiaorg/orchestrator-relayer/).

QGB v1 implementation, including the orchestrator and relayer, is in the [qgb-integration](https://github.com/celestiaorg/celestia-app/tree/qgb-integration) branch.
