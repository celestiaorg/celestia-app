# Fibre Module Specification

## Abstract

The `x/fibre` module enables validators in the active set to register and manage their fibre service provider information.

## Contents

1. [Concepts](#concepts)
2. [State](#state)
3. [Messages](#messages)
4. [Events](#events)
5. [Queries](#queries)
6. [Parameters](#parameters)
7. [Client](#client)

## Concepts

### Fibre Service Provider

Every validator in the active set is a Fibre Service Provider (FSP). Each FSP register's their service address to the celestia-app state. Fibre clients encode data and send unique chunks to each FSP. In return, each FSP signs over a commitment to that data using their consensus key, indicating that they have downloaded it, verified that the encoding is uniquely decodable, and will serve that data upon request for at least the service period.

### State Management

The module maintains a simple key-value store where:
- **Key**: Validator consensus address (celestiavalcons...)
- **Value**: FibreProviderInfo struct containing service details

## State

The `x/fibre` module stores the following data:

### FibreProviderInfo

```protobuf
message FibreProviderInfo {
  // ip_address is the IP address where users can access the fibre service
  string ip_address = 1;
}
```

### Store Keys

- `0x01 | ValidatorConsensusAddress -> ProtocolBuffer(FibreProviderInfo)`: Maps validator consensus address to fibre provider info

## Messages

### MsgSetFibreProviderInfo

Allows a validator to set or update their fibre provider information.

```protobuf
message MsgSetFibreProviderInfo {
  // ip_address is the IP address for the fibre service (max 45 characters for IPv6)
  string ip_address = 1;
}
```

**Validation Rules:**
- `validator_address` must be a valid validator consensus address
- `ip_address` must be non-empty and ≤ 45 characters
- Signer must match the validator operator address for the matching consensus validator address
- Validator must be in the active set

### MsgRemoveFibreProviderInfo

Allows removal of fibre provider information for validators not in the active set.

```protobuf
message MsgRemoveFibreProviderInfo {
  // validator_consensus_address is the consensus address of the validator to remove
  string validator_consensus_address = 1;
}
```

**Validation Rules:**
- `validator_consensus_address` must be a valid validator consensus address
- Validator must NOT be in the active set
- Provider info must exist for the validator

## Events

### EventSetFibreProviderInfo

Emitted when a validator sets or updates their fibre provider information.

```protobuf
message EventSetFibreProviderInfo {
  // validator_consensus_address is the consensus address of the validator
  string validator_consensus_address = 1;
  // ip_address is the IP address for the fibre service
  string ip_address = 2;
}
```

### EventRemoveFibreProviderInfo

Emitted when fibre provider information is removed.

```protobuf
message EventRemoveFibreProviderInfo {
  // validator_address is the consensus address of the validator
  string validator_consensus_address = 1;
}
```

## Queries

### QueryFibreProviderInfo

Query fibre provider information for a specific validator.

**Request:**
```protobuf
message QueryFibreProviderInfoRequest {
  // validator_consensus_address is the consensus address of the validator
  string validator_consensus_address = 1;
}
```

**Response:**
```protobuf
message QueryFibreProviderInfoResponse {
  // info contains the fibre provider information
  FibreProviderInfo info = 1;
  // found indicates if the validator has registered info
  bool found = 2;
}
```

### QueryAllActiveFibreProviders

Query fibre provider information for all validators in the active set.

**Request:**
```protobuf
message QueryAllActiveFibreProvidersRequest {}
```

**Response:**
```protobuf
message QueryAllActiveFibreProvidersResponse {
  // providers contains all active fibre providers
  repeated ActiveFibreProvider providers = 1;
}

message ActiveFibreProvider {
  // validator_consensus_address is the consensus address of the validator
  string validator_consensus_address = 1;
  // info contains the fibre provider information
  FibreProviderInfo info = 2;
}
```

## Parameters

The `x/fibre` module currently defines no parameters. All validation rules are hardcoded.

## Client

### Query Client

To create a query client for the fibre module:

```go
import (
    "context"
    "google.golang.org/grpc"
    fibretypes "github.com/celestiaorg/celestia-app/x/fibre/types"
)

func NewFibreQueryClient(conn *grpc.ClientConn) fibretypes.QueryClient {
    return fibretypes.NewQueryClient(conn)
}

// Query specific validator info
func QueryValidatorInfo(ctx context.Context, client fibretypes.QueryClient, valAddr string) (*fibretypes.QueryFibreProviderInfoResponse, error) {
    req := &fibretypes.QueryFibreProviderInfoRequest{
        ValidatorAddress: valAddr,
    }
    return client.FibreProviderInfo(ctx, req)
}

// Query all active providers
func QueryAllActiveProviders(ctx context.Context, client fibretypes.QueryClient) (*fibretypes.QueryAllActiveFibreProvidersResponse, error) {
    req := &fibretypes.QueryAllActiveFibreProvidersRequest{}
    return client.AllActiveFibreProviders(ctx, req)
}
```

### CLI Commands

**Query Commands:**
```bash
# Query specific validator's fibre info
celestia-appd query fibre provider <validator-consensus-address>

# Query all active fibre providers
celestia-appd query fibre active-providers
```

**Transaction Commands:**
```bash
# Set fibre provider info (must be signed by validator)
celestia-appd tx fibre set-provider-info <ip-address> <consensus-address> --from <validator-operator-key>

# Remove fibre provider info (can be signed by anyone if validator is not active)
celestia-appd tx fibre remove-provider-info <validator-address> --from <remover-key>
```
