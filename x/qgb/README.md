# `x/qgb`

## Abstract

This module contains the [Quantum Gravity Bridge](https://blog.celestia.org/celestiums/) (QGB) state machine implementation.

## State machine

The QGB state machine handles the creation of [`AttestationRequestI`](https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/x/qgb/types/attestation.go#L15-L23) which can be either a [`DataCommitment`](https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/proto/qgb/types.proto#L31-L47) or a [`Valset`](https://github.com/celestiaorg/celestia-app/blob/801a0d412631989ce97748badbd7bb676982db16/proto/qgb/types.proto#L17-L29).

During their creation, the state machine might panic due to an unexpected behavior or event. These panics will be discussed below.

### Conditions of generating a new attestation

#### New valset creation

A new valset is created in the following situations:

```golang
if (latestValset == nil) || (lastUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
    ...
}
```

- No valset exists in store, so a new valset will be created. This happens mostly after genesis, or after a hard fork.
- The current block height is the last unbonding height, i.e. when a validator is leaving the validator set. A new valset will need to be created to accommodate that change.
- A significant power difference happened since the last valset. This could happen if a validator has way more staking power or the opposite. The significant power difference threshold is defined by the constant `SignificantPowerDifferenceThreshold`, and is set to 5% currently.

#### New data commitment creation

A new data commitment is created in the following situation:

```golang
if ctx.BlockHeight() != 0 && ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx)) == 0 {
	...
}
```

I.e. the current block height is not 0, and we're at a data commitment window height.

The data commitment window is defined as a governance parameter that can be changed. Currently, it is set to 400.

### Panics

During EndBlock step, the state machine generates new attestations if needed. During this generation, the state machine could panic.

#### Valset panics

When checking if the state machine needs to generate a new valset, the state machine might panic if it finds invalid state. This can happen in the following cases:

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
   // ref : https://github.com/Gravity-Bridge/Gravity-Bridge/issues/35 
   if errors.Is(err, types.ErrNoValidators) {  
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

When storing a new attestation, the state machine can panic if it finds invalid state. This latter can happen in the following cases:

- The attestation request created is a duplicate of an existing attestation:

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

For QGB v2, the orchestrator and relayer implementations will be in a separate repository. Links will be added subsequently.

QGB v1 implementation, including the orchestrator and relayer, is in the [qgb-integration](https://github.com/celestiaorg/celestia-app/tree/qgb-integration) branch.

QGB ADRs are in the [docs](https://github.com/celestiaorg/celestia-app/tree/main/docs/architecture).
