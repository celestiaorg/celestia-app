# Validator Address Registry

## Abstract

The `x/valaddr` module lets a validator operator register the Fibre server host for its consensus validator. Fibre clients use these records to resolve the gRPC endpoint for validators selected from a validator set.

The module is wired into the app only when the `fibre` build tag is enabled.

## Contents

1. [Concepts](#concepts)
2. [State](#state)
3. [Messages](#messages)
4. [Events](#events)
5. [Queries](#queries)
6. [Parameters](#parameters)
7. [Genesis](#genesis)
8. [Client](#client)

## Concepts

### Fibre Provider Host

A Fibre provider host is the dial target for a validator-operated Fibre gRPC server. The registered value is a canonical `host:port` string. The host portion can be a DNS name, an IPv4 literal, or a bracketed IPv6 literal. Schemes such as `http://` or `dns:///` and URL paths are rejected by message validation.

The registry is keyed by validator consensus address, but registration is submitted by the validator operator address (`celestiavaloper...`). The message handler looks up the staking validator, derives its consensus public key, and stores the host under the derived consensus address.

The registry does not compute validator-set membership. `AllFibreProviders` returns all stored provider records; clients combine these records with the current validator set when selecting providers.

## State

The `x/valaddr` module stores provider info under the module store key `valaddr`.

### FibreProviderInfo

```protobuf
message FibreProviderInfo {
  // host is the network address for the fibre service provider.
  string host = 1;
}
```

### Store Keys

- `0x01 | ValidatorConsensusAddress -> ProtocolBuffer(FibreProviderInfo)`: maps a validator consensus address to its Fibre provider host.

## Messages

### MsgSetFibreProviderInfo

Allows a validator operator to set or update Fibre provider information for its validator.

```protobuf
message MsgSetFibreProviderInfo {
  option (cosmos.msg.v1.signer) = "signer";

  // signer is the validator's operator address (celestiavaloper...).
  string signer = 1 [(cosmos_proto.scalar) = "cosmos.ValidatorAddressString"];

  // host is the network address for the fibre service provider.
  string host = 2;
}
```

Validation rules:

- `signer` must be a non-empty valid validator operator address.
- `host` must be non-empty and at most 100 characters.
- `host` must be in `host:port` form.
- The host part must be non-empty.
- The port must be numeric and in the range `[1, 65535]`.
- The normal transaction path rejects scheme-prefixed or path-bearing hosts because they do not match canonical `host:port` form.
- The message handler requires the validator operator address to exist in the staking keeper, derives the validator consensus address, and stores the host under that consensus address.

## Events

### Set Fibre Provider Info

The proto event shape is:

```protobuf
message EventSetFibreProviderInfo {
  // validator_consensus_address is the validator consensus address (celestiavalcons...).
  string validator_consensus_address = 1;

  // host is the network address for the fibre service provider.
  string host = 2;
}
```

The current message handler emits a plain SDK event:

```text
type: set_fibre_provider_info
attributes:
  validator_consensus_address = <celestiavalcons...>
  host = <host:port>
```

## Queries

The module supports a query for one validator consensus address and a query for all stored provider records.

### FibreProviderInfo

Queries Fibre provider information for a specific validator consensus address.

```protobuf
message QueryFibreProviderInfoRequest {
  // validator_consensus_address is the validator consensus address (celestiavalcons...).
  string validator_consensus_address = 1;
}

message QueryFibreProviderInfoResponse {
  // info contains the fibre provider information.
  FibreProviderInfo info = 1;

  // found indicates if the validator has registered info.
  bool found = 2;
}
```

HTTP gateway route:

```text
GET /valaddr/v1/fibre-provider-info/{validator_consensus_address}
```

### AllFibreProviders

Queries all stored Fibre provider records. This is not filtered to the active validator set and has no pagination argument.

```protobuf
message QueryAllFibreProvidersRequest {}

message QueryAllFibreProvidersResponse {
  // providers contains all fibre providers with a host defined.
  repeated FibreProvider providers = 1;
}

message FibreProvider {
  // validator_consensus_address is the validator consensus address (celestiavalcons...).
  string validator_consensus_address = 1;

  // info contains the fibre provider information.
  FibreProviderInfo info = 2;
}
```

HTTP gateway route:

```text
GET /valaddr/v1/all-fibre-providers
```

## Parameters

The `x/valaddr` module has no parameters.

## Genesis

`GenesisState` is empty. Provider records are not imported from genesis and are not exported into genesis.

```protobuf
message GenesisState {}
```

## Client

### CLI Commands

Query commands:

```bash
# Query one validator's Fibre provider info by consensus address.
celestia-appd query valaddr provider <validator-consensus-address>

# Query all stored Fibre provider records.
celestia-appd query valaddr providers
```

Transaction commands:

```bash
# Set Fibre provider host. The --from key must correspond to the validator operator account.
celestia-appd tx valaddr set-host <host:port> --from <validator-operator-key>
```
