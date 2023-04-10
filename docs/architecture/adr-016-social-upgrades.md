# ADR 016: Removing Token Voting for Upgrades

## Status

Accepted

## Changelog

- 2023/03/15: initial draft

## Context

Standard cosmos-sdk based chains are sovereign and have the ability to hardfork,
but the current implementation makes heavy use of onchain token voting. After a
proposal is submitted and crosses some threshold of agreement, the halt height
is set in the state machine for all nodes. While it's possible for the
validators to modify the binaries used arbitrarily, this sets a social contract,
both on and off chain, to abide by the results of token voting. While the degree
to which using token voting enshrines the influence of large token holders over
that of node operators is debatable, rough social consensus, similar to that of
Bitcoin and Ethereum, is a core component of Celestia governance. Therefore,
this document is exploring some options that attempt to preserve the influence
of node operators.

The most pertinent issue at the moment is determining a safe way to remove the
standard token voting mechanisms from the cosmos-sdk. Celestia mainnet is
quickly approaching, and there is no fully functional alternative upgrade
mechanism in place. The first decision needed is on the mechanism of removing of
token voting2. The latter discussion on how to actually implement upgrades that
fit all of our desired properties is out of scope for this ADR, and will be
discussed separately in
[ADR018](https://github.com/celestiaorg/celestia-app/pull/1562). To summarize
that document, we will be pursuing rolling upgrades that incorporate a TBD
signalling mechanism.

Upgrades that did not use the current cosmos-sdk mechanism have occurred in the
past, even without a signalling mechanism. However, there has not been an
upgrade for a chain with live IBC that has not used the upgrade module. The
solution that is decided on in ADR018 should also consider how IBC should
upgrade, but this document only needs to specifiy which are the minimal changes
needed to remove token voting.

## Alternative Approaches

### Option 1: Temporarily use the current token voting mechanism

Given that we don't have a lot of time until mainnet, we could leave the current
implementation in place which gives us the option to use it if needed. Ideally,
we would work on the longer term upgrade mechanism that respects social
consensus and finish it before the first upgrade. While this option does allow
for maximum flexibility, it is also very risky because if the mechanism is ever
needed or used, then it could set a precedent for future upgrades.

### Option 2: Removal of token voting for upgrades

The goal of this approach is to force social consensus to reach an upgrade
instead of relying on token voting. It works by simply removing the ability of
the gov module to schedule an upgrade. This way, the only way to upgrade the
chain is to agree on the upgrade logic and the upgrade height offchain.

Instead of relying on token voting, a mechanism described in ADR018 for rolling
upgrades and signalling will be used.



### Option 3: Adding a predetermined halt height (aka "difficulty bomb")

This option is not mutually exclusive to option 2. Its goal is to explicitly
state that, without changing binaries, light clients will halt at a given
height, despite what logic validators are running. This acts as an explicit
statement to large token holders that they either come to some sort of agreement
with the rest of the network, or chain will halt. Not coming to agreement is not
a viable option.

## Decision

We will implement option 2 by removing the gov module's ability to schedule an
upgrade.

## Detailed Design

### Implementing Option 1

No changes are needed.

### Implementing Option 2

#### Remove the ability to schedule an upgrade via token voting

Implementing option 2 will involve removing the ability of the gov module to
schedule an upgrade, while maintaining all of the upgrade logic. The upgrade
logic could be removed entirely, but more can be removed later depending on the
decisions made in ADR018

```diff

// New returns a reference to an initialized celestia app.
func New(
   logger log.Logger,
   db dbm.DB,
   traceStore io.Writer,
   loadLatest bool,
   skipUpgradeHeights map[int64]bool,
   homePath string,
   invCheckPeriod uint,
   encodingConfig encoding.Config,
   appOpts servertypes.AppOptions,
   baseAppOptions ...func(*baseapp.BaseApp),
) *App {
   ...

   // register the proposal types
   govRouter := oldgovtypes.NewRouter()
   govRouter.AddRoute(govtypes.RouterKey, oldgovtypes.ProposalHandler).
       AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper)).
       AddRoute(distrtypes.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.DistrKeeper)).
-        AddRoute(upgradetypes.RouterKey, upgrade.NewSoftwareUpgradeProposalHandler(app.UpgradeKeeper)).
       AddRoute(ibcclienttypes.RouterKey, ibcclient.NewClientProposalHandler(app.IBCKeeper.ClientKeeper))
```

To remove the ability to schedule or perform a software upgrades, the following
method and `sdk.Msg` for the message server should be removed. Note that this is
preserving the entire functionality of the upgrade module's keeper, since that
is required to not change the IBC module. This is only removing the ability to
for the state machine to schedule an upgrade.

```proto
// Msg defines the upgrade Msg service.
service Msg {
  // SoftwareUpgrade is a governance operation for initiating a software upgrade.
  //
  // Since: cosmos-sdk 0.46
  rpc SoftwareUpgrade(MsgSoftwareUpgrade) returns (MsgSoftwareUpgradeResponse);
  // CancelUpgrade is a governance operation for cancelling a previously
  // approvid software upgrade.
}

// MsgSoftwareUpgrade is the Msg/SoftwareUpgrade request type.
//
// Since: cosmos-sdk 0.46
message MsgSoftwareUpgrade {
  option (cosmos.msg.v1.signer) = "authority";

  // authority is the address of the governance account.
  string authority = 1 [(cosmos_proto.scalar) = "cosmos.AddressString"];

  // plan is the upgrade plan.
  Plan plan = 2 [(gogoproto.nullable) = false];
}
```

```go
// SoftwareUpgrade implements the Msg/SoftwareUpgrade Msg service.
func (k msgServer) SoftwareUpgrade(goCtx context.Context, req *types.MsgSoftwareUpgrade) (*types.MsgSoftwareUpgradeResponse, error) {
   if k.authority != req.Authority {
       return nil, errors.Wrapf(gov.ErrInvalidSigner, "expected %s got %s", k.authority, req.Authority)
   }

   ctx := sdk.UnwrapSDKContext(goCtx)
   err := k.ScheduleUpgrade(ctx, req.Plan)
   if err != nil {
       return nil, err
   }

   return &types.MsgSoftwareUpgradeResponse{}, nil
}
```

### Implementing Option 3

#### Implement a deadline module into the state machine that will halt at a hardcoded height

This mechanism needs to stop all honest nodes, particularly light clients. This
way validators cannot just ignore the bomb height and continue to produce
blocks. There are multiple different ways to do that, but one universal way to
make sure that all nodes halt would be to include it in the header verification
logic:

```go
// ValidateBasic performs stateless validation on a Header returning an error
// if any validation fails.
//
// NOTE: Timestamp validation is subtle and handled elsewhere.
func (h Header) ValidateBasic() error {
    if h.Height >= consts.BombHeight {
        return errors.New("bomb height reached or exceeded")
    }
    ...
    return nil
}
```

#### Halting the Node using Social Consensus

We hope to perform most upgrades using mechanism that doesn't involve shutting
down and switching binaries, but depending on changes to the code, this might be
difficult or not desirable (note that single binary syncing would still work
fine). In that case, we would still require a mechanism to halt all nodes that
are running the old binary in a way that respects social consensus. One of the
main issues with this approach is that it has a higher halt risk since node
operators could accidently configure this value inconsistently across the
network. We can do that using the existing functionality in the application.
Below is the config in app.toml that would allow node operators to pick a height
to shut down their nodes at.

```toml
# HaltHeight contains a non-zero block height at which a node will gracefully
# halt and shutdown that can be used to assist upgrades and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-height = 0
```

## Consequences

If we adopt Option 2, then we will be able to remove token voting from the state
machine sooner rather than later. This is riskier in that we will not have the
battle tested mechanism, but it will force future upgrades that attempt to give
more influence to node operators and less influence to large token holders.

## References

- [ADR018 Social Upgrades](https://github.com/celestiaorg/celestia-app/pull/1562)
- [ADR017 Single Binary Syncs](https://github.com/celestiaorg/celestia-app/pull/1521)