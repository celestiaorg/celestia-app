# ADR 016: Removing Token Voting for Upgrades

## Status

Accepted

## Changelog

- 2023/03/15: initial draft

## Context

Standard cosmos-sdk based chains are sovereign and have the ability to hardfork, but the current implementation makes heavy use of onchain token voting. This bypasses social consensus, one of the core principles of Celestia. After a proposal is submitted and crosses some threshold of agreement, the halt height is set in the state machine for all nodes, and validators are expected to switch binaries after the state machine halts the node. The ultimate security, ruleset, and result of that ruleset rests solely on the shoulders of node operators, not validators or token holders. Therefore we are seeking a solution that engraves social consensus into the implementation itself.

The most pertinent issue at the moment is that we are launching mainnet soon, and don't have a fully functional upgrade mechanism that empowers social consensus in place. We need to first decide how to remove token voting in a way that supports our future efforts to change the upgrade mechanism. The latter discussion on how to actually implement upgrades that fit all of our desired properties is out of scope for this ADR, and will be discussed separately.

It should also be noted that this ADR is not discussing parameter changes, as limiting token voting on those is also a separate discussion.

## Alternative Approaches

### Option 1: Temporarily use the current token voting mechanism
Given that we don't have a lot of time until mainnet, we could leave the current implementation in place which gives us the option to use it if needed. Ideally, we would work on the longer term upgrade mechanism that respects social consensus and finish it before the first upgrade. While this option does allow for maximum flexibility, it is also very risky because if the mechanism is ever needed or used, then it could set a precedent for future upgrades.

### Option 2: Removal of token voting for upgrades
The goal of this approach is to force social consensus to reach an upgrade instead of relying on token voting. It works by simply removing the current upgrade module. This way, the only way to upgrade the chain is to agree on the upgrade logic and the upgrade height.

### Option 3: Adding a predetermined halt height (aka "difficulty bomb")
This option is not mutually exclusive to option 2. Its goal is to explicitly state that, without changing binaries, light clients will halt at a given height, despite what logic validators are running. This acts as an explicit statement to large token holders that they either come to some sort of agreement with the rest of the network, or chain will halt. Not coming to agreement is not a viable option.

## Decision

We will implement option 2 by removing the upgrade module.

## Detailed Design

### Implementing Option 1

No changes are needed.

### Implementing Option 2

#### Remove the upgrade module from the state machine

This involves a fairly standard removal of a module. Which is summarized (but not all encompassing!) in the following diff:

```diff

// ModuleBasics defines the module BasicManager is in charge of setting up basic,
// non-dependant module elements, such as codec registration
// and genesis verification.
ModuleBasics = module.NewBasicManager(
...
ibc.AppModuleBasic{},
-upgrade.AppModuleBasic{},
evidence.AppModuleBasic{},
...
)

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
-    app.UpgradeKeeper = upgradekeeper.NewKeeper(skipUpgradeHeights, keys[upgradetypes.StoreKey], appCodec, homePath, app.BaseApp, authtypes.NewModuleAddress(govtypes.ModuleName).String())
   ...

   // register the proposal types
   govRouter := oldgovtypes.NewRouter()
   govRouter.AddRoute(govtypes.RouterKey, oldgovtypes.ProposalHandler).
       AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper)).
       AddRoute(distrtypes.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.DistrKeeper)).
-        AddRoute(upgradetypes.RouterKey, upgrade.NewSoftwareUpgradeProposalHandler(app.UpgradeKeeper)).
       AddRoute(ibcclienttypes.RouterKey, ibcclient.NewClientProposalHandler(app.IBCKeeper.ClientKeeper))
```

Removing the [upgrade module](https://github.com/celestiaorg/cosmos-sdk/tree/v1.8.0-sdk-v0.46.7/x/upgrade) will also remove the `sdk.Msg` that is used by the (non-legacy) governance module, along with any capacity to halt. After this, both the legacy and the current governance modules will no longer have the ability to call the below function:

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

This mechanism needs to stop all honest nodes, particularly light clients. This way validators cannot just ignore the bomb height and continue to produce blocks. There are multiple different ways to do that, but one universal way to make sure that all nodes halt would be to include it in the header verification logic:

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

We hope to perform most upgrades using mechanism that doesn't involve shutting down and switching binaries, but depending on changes to the code, this might be difficult or not desirable (note that single binary syncing would still work fine). In that case, we would still require a mechanism to halt all nodes that are running the old binary in a way that respects social consensus. We can do that using the existing functionality in the application. Below are the configs in app.toml that would allow node operators to pick a height to shutdown their nodes at.

```toml
# HaltHeight contains a non-zero block height at which a node will gracefully
# halt and shutdown that can be used to assist upgrades and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-height = 0

# HaltTime contains a non-zero minimum block time (in Unix seconds) at which
# a node will gracefully halt and shutdown that can be used to assist upgrades
# and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-time = 0
```

## Consequences

If we adopt Option 2, then we will be able to remove token voting from the state machine sooner rather than later. This is riskier in that we will not have the battle tested mechanism, but it will force future upgrades that respect social consensus.

## References
