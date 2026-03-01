# Validator Address Registry

## Abstract

The `x/valaddr` module enables validators to register their fibre service provider information.

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

Every validator in the active set is a Fibre Service Provider (FSP). Each FSP register's their service addresses to the celestia-app state. Fibre clients encode data and send unique chunks to each FSP. In return, each FSP signs over a commitment to that data using their consensus key, indicating that they have downloaded it, verified that the encoding is uniquely decodable, and will serve that data upon request for at least the service period.

### State Management

The module maintains a simple key-value store where:

- **Key**: Validator consensus address (celestiavalcons...)
- **Value**: FibreProviderInfo struct containing service details

## State

The `x/valaddr` module stores the following data:

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
  string signer = 1;
  // host is the network address for the fibre service provider (max 90 characters)
  string host = 2;
}
```

**Validation Rules:**

- `signer` must be a valid validator consensus operator address
- `host` must be less than 90 characters

If the `signer` is currently jailed, this will attempt to unjail them

## Failure to provide fibre info

While the network can not enforce that every validator is running an available fibre DA server, it can enforce that all validators
in the active set have supplied an IP address. To do this, the network employs the `AfterValidatorBonded` hook in the staking module for any validator that gets added to the active set. If no info is provided, the validator is jailed. For existing active validators, at height **missing_info_check_height**, aimed at 1 week after the upgrade height, in `EndBlock` all active validators will be checked for missing info, those that have not supplied info will be jailed.

To bypass the usage `MsgUnjail`, jailing will be for 1 year from the current blocktime. To unjail, a validator must submit the `MsgSetFibreProviderInfo` only then will the `JailedUntil` be updated and `Unjail` called.

## Events

### EventSetFibreProviderInfo

Emitted when a validator sets or updates their fibre provider information.

```protobuf
message EventSetFibreProviderInfo {
  // validator_consensus_address is the consensus address of the validator
  string validator_consensus_address = 1;
  // ip_address is the IP addresses for the fibre service provider
  string ip_address = 2;
}
```

## Queries

The module supports two types of queries. The first one is aimed for new fibre clients to build their address book. The second
is to request the info for specific providers when a) they are added to the validator set or b) they are unreachable and thus the address may have changed.

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
  repeated FibreProvider providers = 1;
}

message FibreProvider {
  // validator_consensus_address is the consensus address of the validator
  string validator_consensus_address = 1;
  // info contains the fibre provider information
  FibreProviderInfo info = 2;
}
```

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

## Parameters

The `x/valaddr` has the following parameter. These can only be adjusted in hard-fork upgrades

- **missing_info_check_height** [int64]: when to check for validators with missing info. This acts as a grace period and can be dynamically set in the migration. (default: **100,000** - this is roughly 1 week from the upgrade height)

## Client

### CLI Commands

**Query Commands:**

```bash
# Query specific validator's fibre info
celestia-appd query fibre provider <validator-consensus-address>

# Query all active fibre providers
celestia-appd query fibre providers <num-providers>
```

**Transaction Commands:**

```bash
# Set fibre provider info (must be signed by validator)
celestia-appd tx fibre set-host <host-address> --from <validator-operator-key>
```
