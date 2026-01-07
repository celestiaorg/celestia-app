## x/forwarding module spec

### Overview
The `x/forwarding` module bridges Hyperlane messages into Celestia by
registering interchain accounts (ICA) routers and validating inbound
Hyperlane messages before dispatching embedded Cosmos SDK calls.

Key goals:
- register a router keyed by a Hyperlane mailbox id and owner
- enroll remote router bindings for specific destination domains
- validate Hyperlane-originated messages and decode ICA payloads

### State
Stored under `x/forwarding` KV store using collections.

- Routers: `RoutersKeyPrefix` (`0`)
  - key: `uint64` router id (internal id from `util.HexAddress`)
  - value: `types.InterchainAccountsRouter`
- RemoteRouters: `RemoteRoutersKeyPrefix` (`1`)
  - key: `(uint64 router id, uint32 domain)`
  - value: `types.RemoteRouter`

### Messages

`MsgCreateInterchainAccountsRouter`
- creates a router and enrolls the first remote router binding
- checks the mailbox exists in the Hyperlane keeper
- assigns the next sequence id from the Hyperlane app router
- stores `InterchainAccountsRouter` + `RemoteRouter`

`MsgEnrollRemoteRouter`
- registers a remote router binding for a router id + domain
- not yet implemented in keeper logic

`MsgWarpForward`
- derives a forwarding address from destination domain + recipient
- builds a Hyperlane warp `MsgRemoteTransfer` and routes it
- event propagation is preserved from the routed message handler

### Payload handling
`ParseInterchainAccountsPayload` decodes ICA payloads from Hyperlane
messages. The layout mirrors Hyperlane's ICA message format in Solidity:
https://github.com/hyperlane-xyz/hyperlane-monorepo/blob/main/solidity/contracts/middleware/libs/InterchainAccountMessage.sol

Currently supported:
- CALLS payloads only (COMMITMENT is unsupported)
- validates the first byte as `MessageType.CALLS`
- extracts fixed-width `owner`, `ism`, and `salt` fields (bytes32 each)
- ABI-decodes the remaining `Call[]` payload
- unmarshals each `Call.Data` into a `codectypes.Any` for SDK dispatch

Important notes:
- the sender contract is validated against the enrolled remote router
- the origin mailbox id is enforced against the router's configuration
- decoding assumes the call data is protobuf-encoded `Any` messages

### Hyperlane integration
The keeper implements `util.HyperlaneApp`:
- `Exists` checks router presence
- `Handle` validates mailbox, remote router, and sender contract
- `ReceiverIsmId` resolves per-router ISM or mailbox default

### CLI
Tx commands are provided under the module root:
- `create-ica-router [origin-mailbox] [receiver-domain] [receiver-contract] [gas]`
  - optional flag: `--ism-id` (hex address)

### Genesis
Genesis state is currently empty and only validates structure.

### Queries
No gRPC or CLI queries are exposed yet.
