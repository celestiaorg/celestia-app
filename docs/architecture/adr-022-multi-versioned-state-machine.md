# ADR 022: Multi-Versioned State Machine

## Changelog

- 2024/03/15: Initial draft (@cmwaters)

## Status

Implemented

## Context

The Celestia application required a modification from the existing Cosmos SDK to support multiple versions of the state machine simultaneously. This capability is crucial for single binary syncs and ensuring a smooth transition during upgrades, allowing nodes to upgrade independently and then switch to the next state machine without any downtime to the network. This is important for a network on which a large number of rollups and users depend.

## Decision

We decided to implement a multi-versioned state machine by making significant modifications to the module manager, the configurator, and introducing a new ante handler. These changes enable the application to maintain and execute different versions of the state machine based on the app version associated with each transaction and prevents the execution of transactions that are not part of the current state machine.

## Detailed Design

There are several parts of the protocol that were modified.

### Loading and Setting the App Version

The app version was previously never persisted to disk, but was simply hardcoded into the application (as 1). An application with multiple versions needs to be able to persist them to disk and load them in the case the node crashes or is shutdown. The `Info` ABCI call must pass the app version to Comet for Comet to generate headers with it. The decision was made to always require the app version to be set in the consensus params of Genesis (prior `ConsensusParams` wasn't mandatory). Upon first use, `Info` will return 0 (as no app version has been persisted) and then take the app version passed in `InitChain` to initialize the app version. It is persisted both to disk in the `ParamStore` and in memory in the `BaseApp` struct. Upon later startups, `Info` will load the app version from disk, set it in memory and also return it to Comet. The `AppVersion` is only later adjusted when `Upgrade` is called on the `Application`. Upgrading is no longer a module but is more a native part of the application. Modules register migrations and `Upgrade`, which is executed in `EndBlock` will trigger those migrations and then set the new version. Upgrading needs to happen in `EndBlock`, such that `PrepareProposal` for the following height already is run with the logic of the new version.

### SDK Context

The `sdk.Context` type is passed to almost every component in the application and is thus ideal for storing and accessing the current app version. It already contains the app version in the block header. However, this is not always populated. Thus `InitChain` and `PrepareProposal` were modified to include the app version as either passed from the genesis file or loaded from the application itself.

### Module Manager Changes

The module manager was modified to contain a mapping of modules to their version.

```go
type Manager struct {
    versionedModules map[uint64]map[string]sdkmodule.AppModule
    ...
}
```

Each module has a `Name` and a `ConsensusVersion` that marks it unique. No two modules can have the same `Name` within a single app version. When constructing the application, we specify the contiguous range (inclusive) of app versions that the module is part of:

```go
app.mm, err = module.NewManager([]module.VersionedModule{
    {
        Module:      blobstream.NewAppModule(appCodec, app.BlobstreamKeeper),
        FromVersion: v1, ToVersion: v2,
    },
    {
        Module:      upgrade.NewAppModule(app.UpgradeKeeper),
        FromVersion: v2, ToVersion: v2,
    },
})
```

`InitGenesis`, `BeginBlock` and `EndBlock` will only be called for the modules that belong to the current version of the application.

`DeliverTx` is not called by the `module.Manager`. To ensure that only relevant transactions are executed, the `MsgServiceRouter` had to be modified to reject the execution of transaction that are not part of the app version through the `CircuitBreaker` struct within it.

### Configurator Changes

The configurator is an object that is used upon initialisation to register the msg server, query server and migrations. In order to know what `sdk.Msg`s corresponded with which app version, a wrapper was added to the configurator that would scrape all the `sdk.Msg`s from the msg server as they were being registered and add them to a map:

```go
// acceptedMsgs is a map from appVersion -> msgTypeURL -> struct{}.
acceptedMessages map[uint64]map[string]struct{}
```

### Transaction Filtering through the `MsgVersioningGateKeeper`

The messages within a transaction are filtered based on the app version. The map constructed from the configurator is passed to a new AnteHandler which will reject transactions of a different app version in `CheckTx` so that clients get an immediate response. The `MsgVersioningGateKeeper` is also passed to the `MsgServiceRouter` to reject transactions upon execution. The reason for needing both is that another module may try to execute a transaction for a module that is not part of the app version.

## Alternative Approaches

At the broadest level, the handling of multiple state machines does not have to be done within a single binary but could be orchestrated by a binary manager that upon some trigger, stops one state machine and starts up the next.

However, having it in a single binary provides less surface area for mistakes by the node operator. It also makes it straightforward to test upgrades and to make the transition more reliable. In the event of a failed migration, it is easy to simply continue with the old version.

## Consequences

### Positive

- **Single Binary Sync**: The multi-versioned state machine ensures that the network can process transactions from different app versions. This allows a node to start from genesis and sync to the head without disruption.
- **Smooth Upgrades**: Upgrades can be rolled out more smoothly, with nodes transitioning to new versions of the state machine without downtime to the entire network

### Negative

- **Increased Complexity**: Managing multiple versions of the state machine and ensuring compatibility across versions adds complexity to the application's architecture.

### Neutral

- **Downgrades**: Although not supported in this ADR, it is possible that the application be able to downgrade in the event of some vulnerability or that migrations are unsuccessful.

## References

- N/A
