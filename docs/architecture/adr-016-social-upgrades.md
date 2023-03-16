# ADR 016: Removing Token Voting for Upgrades

## Status

Proposed

## Changelog

- 2023/03/15: initial draft

## Context

Standard cosmos-sdk based chains are sovereign and have the ability to hardfork, but the current implementation makes heavy use of onchain token voting. This bypasses social consensus, one of the core principles of Celestia. After a proposal is submitted and crosses some threshold of agreement, the halt height is set in the state machine for all nodes, and validators are expected to switch binaries after the state machine halts the node. The ultimate security, ruleset, and result of that ruleset rests solely on the shoulders of node operators, not validators or token holders. Therefore we are seeking a solution that engraves social consensus into the implementation itself.

The most pertinent issue at the moment is that we are launching mainnet soon, and don't have a fully functional upgrade mechanism that empowers social consensus in place. We need to first decide how to remove token voting in a way that supports our future efforts to change the upgrade mechanism. The latter discussion on how to actually implement upgrades that fit all of our desired properties is out of scope for this ADR, and will be discussed separately.

It should also be noted that this ADR is not discussion parameter changes, as limiting token voting on those is also a separate discussion.

## Alternative Approaches

### Option 1: Temporarily use the current token voting mechanism

Given that we don't have a lot of time until mainnet, we could leave the current implementation in place which gives us the option to use it if needed. Ideally, we would work on the longer term upgrade mechanism that respects social consensus and finish it before the first upgrade. While this option does allow for maximum flexibility, it is also very risky because if the mechanism is ever needed or used, then it could set a precedent for future upgrades.

### Option 2: Predetermined halt height and removal of token voting for upgrades (aka "difficulty bomb")

The goal of this approach is to set a deadline to upgrade via social consensus. It involves removing the upgrade mechanism from the state machine, and replaces it with a predefined shut down height. This change should be added to all node types, including light clients. If this height is reached without first upgrading, then the chain will halt for all participants.

## Decision

> This section records the decision that was made.
> It is best to record as much info as possible from the discussion that happened. This aids in not having to go back to the Pull Request to get the needed information.

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

#### Implement a deadline module into the state machine that will halt at a hardcoded height

There are a few different mechanisms that we could use to add a constant that
would halt all nodes in the network. This could be as simple as:

```go
func BeginBlocker(k keeper.Keeper, ctx sdk.Context, _ abci.RequestBeginBlock) {
   if ctx.BlockHeader().Height >= BombHeight {
       panic("social consensus must be reached to pass the bomb height")
   }
   return
}
```

We could also impose other similar restrictions elsewhere instead, such as voting to reject the block if the deadline height is `BombHeight` is reached.

```go
func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
    if req.Header.Height >= consts.BombHeight {
        logInvalidPropBlock(app.Logger(), req.Header, "bomb height reached")
        return abci.ResponseProcessProposal{
            Result: abci.ResponseProcessProposal_REJECT,
        }
    }
    ...
}
```

Most importantly, we would also need to add this bomb height to light clients. This way validators cannot just ignore the bomb height and continue to produce blocks. There are multiple different ways to do that, but one universal way to make sure that all nodes, including light, halt at the bomb height would be to include it in the header verification logic:

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

If we adopt Option 2, then we will be able to remove token voting from the statemachine sooner rather than later. This is riskier in that we will not have the battle tested mechanism, but it will set a precedent for future upgrades that respect social consensus.

## References
