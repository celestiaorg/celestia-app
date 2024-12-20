# ADR 004: QGB Relayer Security

## Status

Implemented

## Changelog

- 2022-06-05: Synchronous QGB implementation

## Context

The current QGB design requires relayers to relay everything in perfect synchronous order, but the contracts do not.
In fact, the QGB smart contract is designed to update the data commitments as follows:

- Receive a data commitment
- Check that the block height (nonce) is higher than the previously committed root
- Check if the data commitment is signed using the current valset _(this is the problematic check)_
- Then, other checks + commit

So, if a relayer is up to date, it will submit data commitment and will pass the above checks.

Now, if the relayer is missing some data commitments or valset updates, then it will start catching up the following way:

- Relay valset
- Keep relaying all data commitments that were signed using that valset
- If a new valset is found, check that: up to the block where the valset was changed, all the data commitments that happened during that period are relayed
- Relay the next valset
- And, so on.

The problem with this approach is that there is a constant risk for any relayer to mess up the ordering of the attestations submission, ie relaying the next valset before relaying all the data commitments that were signed using the previous valset, and ending up with signatures holes.

Also, a malicious relayer, can target any honest QGB relayer in normal mode, or while catching up, and mess its attestations submission order, as follows:

- Get the latest relayed valset
- Listen for new signed valsets
- Once a new valset is signed by 2/3 of the network, submit it immediately to be included in next block without waiting for the data commitments to be relayed also

Then, this would create holes in the signatures as the honest relayer will no longer be able to submit the previous data commitments as they're not, in general, signed by the valset relayed by the malicious relayer. And the only solution is to jump to the ones that were signed with the current valset.

## Alternative Approaches

### More synchrony: Deploy the QGB contract with a data commitment window

When deploying the QGB  contract,  also set the data commitment window,  ie, the number of blocks between the `beginBlock` and `endBlock` of each data commitment confirms.

Then, update the QGB contract to check when receiving a new valset if the latest relayed data commitment height is >= new valset height - data commitment window.

This also would mean adding, for example, a `DataCommitmentWindowConfirm` representing signatures of the validator set for a certain `DataCommitmentWindow`, since this latter can be updated using gov proposals.

- Cons:
  - More complexity and state in the contract
- Pros:
  - Fix the race condition issue

### Synchronous QGB: Universal nonce approach

This approach consists of switching to a synchronous QGB design utilizing universal nonces. This means the `ValsetConfirm`s and `DataCommitmentConfirm`s will have the same nonce being incremented on each attestation. Then, the QGB contract will check against this universal nonce and only accept an attestation if its nonce is incremented by 1.

- Cons:
  - Unifying the `ValsetConfirm`s and `DataCommitmentConfirm`s under the same nonce even if they represent separate concepts.
- Pros:
  - Simpler QGB smart contract

### Add more state to the contract: Store valsets and their nonce

Update the QGB contract to store the valset hashes + their nonces:

- Cons:
  - Would make the contract more complex
- Pros:
  - Would make the relayer parallelizable (can submit data commitments and valsets in any order as long as the valset is committed)
  - would allow the QGB to catchup correctly even in the existence of a malicious relayer

### A request-oriented design

Currently, the attestations that need to be signed are defined by the state machine based on `abci.endBlock()` and a `DataCommitmentWindow`. This simplifies the state machine and doesn't require implementing new transaction types to ask for orchestrators' signatures.

The request-oriented design means providing users (relayers mainly) with the ability to post data commitment requests and ask orchestrators to sign them.

The main issue with this approach is spamming and state bloat. In fact, allowing attestation signatures requests would allow anyone to spam the network with unnecessary signatures and make orchestrators do unnecessary work. This gets worse if signatures are part of the state since this latter is costly.

A proposition to remediate the issues described above is to make the signatures part of the block data in specific namespaces. Then, we can charge per request and even make asking for attestations signatures a bit costly.

- Pros
  - Would allow anyone to ask for signatures over commitments, ie, the QGB can then be used by any team without changing anything.
- Cons
  - Makes slashing more complicated. In fact, to slash for liveness, providing the whole history of blocks, proving that a certain orchestrator didn't sign an attestation in the given period, will be hard to implement and the proofs will be big. Compared to the attestations being part of the state, which can be queried easily.

## Decision

The **Synchronous QGB: Universal nonce approach** will be implemented as it will allow us to ship a working QGB 1.0 version faster while preserving the same security assumptions at the expense of parallelization, and customization, as discussed under the _request-oriented design_ above.

## Detailed Design

### AttestationRequestI

The **Synchronous QGB: Universal nonce approach** means that the data commitment requests and valset requests will be ordered following the same nonce.

In order to achieve this, we will need to either:

- Have a separate nonce for each and define ordering conditions when updating them to guarantee the universal order.
- Define an abstraction of the data commitment requests and valsets that guarantees the order. Then, link it to the concrete types.

In our implementation, we will go for the second approach.

#### Interface implementation

To do so, we will first need to define the interface `AttestationRequestI`:

```go
type AttestationRequestI interface {
    proto.Message
    codec.ProtoMarshaler
    Type() AttestationType
    GetNonce() uint64
}
```

This interface implements the `proto.Message` so that it can be handled by protobuf messages. Also, it implements the `codec.ProtoMarshaler` to be marshalled/unmarshaled by protobuf.

Then, it contains a method `Type() AttestationType` which returns the attestation type which can be one of the following:

- `DataCommitmentRequestType`: for data commitment requests
- `ValsetRequestType`: for valset requests

```go
type AttestationType int64

const (
    DataCommitmentRequestType AttestationType = iota
    ValsetRequestType
)
```

Finally, a method `GetNonce() uint64` keeps track of the nonce and returns it.

#### Protobuf implementation

On the proto files, we will use the following notation to refer to the `AttestationRequestI` defined above:

```protobuf
google.protobuf.Any attestation = 1
     [ (cosmos_proto.accepts_interface) = "AttestationRequestI" ];
```

This allows us to query for the attestations using nonces without worrying about the underlying implementation.

For example:

```protobuf
message QueryAttestationRequestByNonceRequest { uint64 nonce = 1; }

message QueryAttestationRequestByNonceResponse {
  google.protobuf.Any attestation = 1
      [ (cosmos_proto.accepts_interface) = "AttestationRequestI" ];
}
```

And, implement the query as follows:

```go
func (k Keeper) AttestationRequestByNonce(
    ctx context.Context,
    request *types.QueryAttestationRequestByNonceRequest,
) (*types.QueryAttestationRequestByNonceResponse, error) {
    attestation, found, err := k.GetAttestationByNonce(
        sdk.UnwrapSDKContext(ctx),
        request.Nonce,
    )
    if err != nil {
        return nil, err
    }
    if !found {
        return &types.QueryAttestationRequestByNonceResponse{}, types.ErrAttestationNotFound
    }
    val, err := codectypes.NewAnyWithValue(attestation)
    if err != nil {
        return nil, err
    }
    return &types.QueryAttestationRequestByNonceResponse{
        Attestation: val,
    }, nil
}
```

#### State machine

On the state machine, we will need to store the attestations when needed. To do so, we will define the following:

##### Store the latest nonce

We will need to keep track of the latest nonce to enforce the nonces order and avoid overwriting existing attestations. This will be done using the following:

```go
func (k Keeper) SetLatestAttestationNonce(ctx sdk.Context, nonce uint64) {
    if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx)+1 != nonce {
        panic("not incrementing latest attestation nonce correctly!")
    }

    store := ctx.KVStore(k.storeKey)
    store.Set([]byte(types.LatestAttestationNonce), types.UInt64Bytes(nonce))
}
```

This will **panic** in the following cases:

- The nonce we are incrementing does not exist. The following method checks its existence:

```go
func (k Keeper) CheckLatestAttestationNonce(ctx sdk.Context) bool {
    store := ctx.KVStore(k.storeKey)
    has := store.Has([]byte(types.LatestAttestationNonce))
    return has
}
```

- The provided nonce is different than `Latest nonce + 1`.

##### Store attestation

The following will store the attestation given that the nonce has never been used before. If not, the state machine will **panic**:

```go
func (k Keeper) StoreAttestation(ctx sdk.Context, at types.AttestationRequestI) {
    nonce := at.GetNonce()
    key := []byte(types.GetAttestationKey(nonce))
    store := ctx.KVStore(k.storeKey)

    if store.Has(key) {
        panic("trying to overwrite existing attestation request")
    }

    b, err := k.cdc.MarshalInterface(at)
    if err != nil {
        panic(err)
    }
    store.Set((key), b)
}
```

The `GetAttestationKey(nonce)` will return the key used to store the attestation, which is defined as follows:

```go
// AttestationRequestKey indexes valset requests by a nonce
AttestationRequestKey = "AttestationRequestKey"

// GetAttestationKey returns the following key format
// prefix    nonce
// [0x0][0 0 0 0 0 0 0 1]
func GetAttestationKey(nonce uint64) string {
    return AttestationRequestKey + string(UInt64Bytes(nonce))
}
```

Also, we will define a `SetAttestationRequest` method that will take an attestation and store it while also updating the `LatestAttestationNonce` and emitting an event. This will allow us to always update the nonce and not forget about it:

```go
func (k Keeper) SetAttestationRequest(ctx sdk.Context, at types.AttestationRequestI) error {
    k.StoreAttestation(ctx, at)
    k.SetLatestAttestationNonce(ctx, at.GetNonce())

    ctx.EventManager().EmitEvent(
        sdk.NewEvent(
            types.EventTypeAttestationRequest,
            sdk.NewAttribute(sdk.AttributeKeyModule, types.ModuleName),
            sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(at.GetNonce())),
        ),
    )
    return nil
}
```

Then, define eventual getters that will be used to serve orchestrator/relayer queries.

### ABCI

In order for the attestations requests to be stored correctly, we will need to enforce some rules that will define how these former will be handled. Thus, we will use `EndBlock` to check the state machine and see whether we need to create new attestations or not.

To do so, we will define a custom `EndBlocker` that will be executed at the end of every block:

```go
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
    handleDataCommitmentRequest(ctx, k)
    handleValsetRequest(ctx, k)
}
```

#### handleDataCommitmentRequest

Handling the data commitment requests is fairly easy. We just check whether we reached a new data commitment window and we need to create a new data commitment request.

The data commitment window is defined as a parameter:

```protobuf
message Params {
  ...
  uint64 data_commitment_window = 1;
}
```

And set during genesis.

So, we will have the following:

```go
func handleDataCommitmentRequest(ctx sdk.Context, k keeper.Keeper) {
    if ctx.BlockHeight() != 0 && ctx.BlockHeight()%int64(k.GetDataCommitmentWindowParam(ctx)) == 0 {
        dataCommitment, err := k.GetCurrentDataCommitment(ctx)
        if err != nil {
            panic(sdkerrors.Wrap(err, "couldn't get current data commitment"))
        }
        err = k.SetAttestationRequest(ctx, &dataCommitment)
        if err != nil {
            panic(err)
        }
    }
}
```

Which will get the current data commitment  as follows:

```go
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
    beginBlock := uint64(ctx.BlockHeight()) - k.GetDataCommitmentWindowParam(ctx)
    endBlock := uint64(ctx.BlockHeight())
    nonce := k.GetLatestAttestationNonce(ctx) + 1

    dataCommitment := types.NewDataCommitment(nonce, beginBlock, endBlock)
    return *dataCommitment, nil
}
```

And store it.

#### handleValsetRequest

The `handleValsetRequest` is more involved as it has more criteria to create new valsets:

```go
func handleValsetRequest(ctx sdk.Context, k keeper.Keeper) {
    // get the last valsets to compare against
    var latestValset *types.Valset
    if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) != 0 {
        var err error
        latestValset, err = k.GetLatestValset(ctx)
        if err != nil {
            panic(err)
        }
    }

    lastUnbondingHeight := k.GetLastUnBondingBlockHeight(ctx)

    significantPowerDiff := false
    if latestValset != nil {
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
        intCurrMembers, err := types.BridgeValidators(vs.Members).ToInternal()
        if err != nil {
            panic(sdkerrors.Wrap(err, "invalid current valset members"))
        }
        intLatestMembers, err := types.BridgeValidators(latestValset.Members).ToInternal()
        if err != nil {
            panic(sdkerrors.Wrap(err, "invalid latest valset members"))
        }

        significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers) > 0.05
    }

    if (latestValset == nil) || (lastUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
        // if the conditions are true, put in a new validator set request to be signed and submitted to Ethereum
        valset, err := k.GetCurrentValset(ctx)
        if err != nil {
            panic(err)
        }
        err = k.SetAttestationRequest(ctx, &valset)
        if err != nil {
            panic(err)
        }
    }
}
```

In a nutshell, a new valset will be emitted if any of the following is true:

- The block is the genesis block and no previous valsets were defined. Then, a new valset will be stored referencing the validator set defined in genesis.
- We're at an unbonding height and we need to update the valset not to end up with a valset containing validators that will cease to exist.
- A significant power difference occurred, i.e. the validator set changed significantly. This is defined using the following:

```go
significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers) > 0.05
```

## References

- Tracker issue for the tasks [here](https://github.com/celestiaorg/celestia-app/issues/467).
