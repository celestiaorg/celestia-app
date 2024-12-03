# IBC Token Filter

## Abstract

The IBC token filter prevents non-native tokens from being transferred through the IBC transfer module from a counterparty chain to the host chain. This is useful if a chain wishes to only permit
their native token within their state machine.

## Context

When tokens are transferred through the IBC transfer module, the denomination of that token is modified at each hop. It's more accurate to think of the tokens transferring to another network to actually remain held in escrow at the origin chain and the destination chain minting a "wrapped" equivalent. Thus tokens moved to a non-native state machine are beholdent to the security of both chains. The denomination is denoted as the path it's taken from the origin chain. To provide an example, imagine 3 chains: A, B and C with native tokens "a", "b", "c". With IBC's transfer module, when a user transfers token "a" from A to B, the wrapped token is prefixed with the source port and source channel (i.e. `portidone/channel-0/a`), this is Ba. If the token is further transferred to C it becomes BCa (or `portidtwo/channel-1/portidone/channel-0/a`). This token is now beholden to the security of B, C and A. Also note that this is different if "a" were to go directly to C. In other words: `Ca != BCa`.

This context is important in recognising when a native token is returning to its origin state machine. Each IBC packet contains metadata including the source port and channel. Therefore if the denomination of the token is prefixed with the same source port and channel as detailed in the packet, we can conclude that the denomination originally came from the receiving chain.

This logic is employed for path unwinding. Reversing the tokens through the path it came will eventually strip the denomination until it reaches the base denomination it started with.

## Protocol

The protocol targets inbound packets only. Outbound transfer messages are left unmodified. When a packet is sent to the IBC transfer module (denoted by the `transfer` port id), the token filter intercepts the message and attempts to unmarshal it as a `FungibleTokenPacketData`. If unmarshalling fails, the protocol should simply pass it down the stack for it to be handled elsewhere.

When tokens are transferred the token filter checks if the denomination is prefixed with the source port and channel of the packet. If so it passes the packet along for the transfer module to handle, else it returns a new error acknowledgement which will be returned to the sending chain.

The protocol does not check the length of the path that prefixes the base denomination i.e. it may still contain multiple ports and channels like `portidtwo/channel-1/portidone/channel-0/a`. This means that it may not be the native token but any other token that had previously passed through the state machine. This means if a chain were to adopt the middleware with existing state, the prior tokens may still unwind through that chain. For chains that commence using this middleware, no other token but the native denominations will be present.

## Implementation

The token filter is implemented as IBC middleware. It wraps the IBC transfer module. All other methods get routed directly to the underlying transfer module except for `OnRecvPacket` which adds extra logic before calling the `OnRecvPacket` method of the transfer module.

The transfer module already includes a `ReceiverChainIsSource` method. The basic logic is therefore:

```go
if transfertypes.ReceiverChainIsSource(packet.GetSourcePort(), packet.GetSourceChannel(), data.Denom) {
	return m.IBCModule.OnRecvPacket(ctx, packet, relayer)
}
return channeltypes.NewErrorAcknowledgement("denomination not accepted by this chain")
```
