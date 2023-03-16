# ADR 016: Forcing Social Upgrades

## Status

Proposed

## Changelog

- 2023/03/15: initial draft

## Context

Standard cosmos-sdk based chains are soverign and have the ability to hardfork, but the current implementation makes heavy use of onchain token voting. After a proposal is submitted and crosses some threshold of aggreement, the halt height is set in the state machine for all nodes, and validators are expected to switch binaries after the state machine halts the node. This mechanism does work, but it bypasses social consensus, one of the core principles of Celestia. The ultimate security, ruleset, and result of that ruleset rests soley on the shoulders of node operators, not validators or token holders. Therefore we are seeking a solution that engraves social consensus into the implementation itself.

The most pertinent issue at the moment is that we are launching mainnet soon, and don't have a fully functional upgrade mechanism that empowers social consensus in place. We need to first decide on a short term solution that will work for mainnet, and later decide on and implement a long term solution. The latter discussion on how to actually implement upgrades that fit all of our desired properties is out of scope for this ADR, and will be discussed in a separate ADR.

It should also be noted that this ADR is not discussion parameter changes, as limiting token voting on those is also a separate discussion.

## Alternative Approaches

### Option 1: Temporarily use the current token voting mechansism

Given that we don't have a lot of time until mainnet, we could leave the current implementation in place which gives us the option to use it if needed. Ideally, we would work on the longer term upgrade mechanism that respects social consensus and finish it before the first upgrade. While this option does allow for maximum flexibility, it is also very risky because if the mechanism is ever needed or used, then it could set a precedent for future upgrades.

### Option 2: Predetermined halt height and removal of token voting for upgrades (aka "difficulty bomb")

The goal of this approach is to set a deadline to reach social consensus. It involves removing the upgrade mechanism from the state machine, and replaces it with a predefined shut down height. If this height is reached without first upgrading, then the chain will halt. Without an onchain voting mechanism, the only way to upgrade the chain is to reach social consensus. If we don't collectively do that, then the chain will halt.

## Decision

> This section records the decision that was made.
> It is best to record as much info as possible from the discussion that happened. This aids in not having to go back to the Pull Request to get the needed information.

## Detailed Design

### Implementing Option 1

No changes are needed.

### Implementing Option 2

- Remove the upgrade module from the state machine

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
-	app.UpgradeKeeper = upgradekeeper.NewKeeper(skipUpgradeHeights, keys[upgradetypes.StoreKey], appCodec, homePath, app.BaseApp, authtypes.NewModuleAddress(govtypes.ModuleName).String())
    ...

	// register the proposal types
	govRouter := oldgovtypes.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, oldgovtypes.ProposalHandler).
		AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper)).
		AddRoute(distrtypes.RouterKey, distr.NewCommunityPoolSpendProposalHandler(app.DistrKeeper)).
-		AddRoute(upgradetypes.RouterKey, upgrade.NewSoftwareUpgradeProposalHandler(app.UpgradeKeeper)).
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

- Implement a deadline module/hook into the state machine that will halt at a predetermined height
This could be as simple as:

```go
func BeginBlocker(k keeper.Keeper, ctx sdk.Context, _ abci.RequestBeginBlock) {
	if ctx.BlockHeader().Height >= BombHeight {
		panic("social consensus must be reached to pass the bomb height")
	}
    return 
}
```

## Consequences

### Positive

### Negative

### Neutral

## References

> Are there any relevant PR comments, issues that led up to this, or articles referenced for why we made the given design choice? If so link them here!

- {reference link}
