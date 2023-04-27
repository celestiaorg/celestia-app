# `x/version`

## Abstract

The version module will increment the app version in the header per a predefined
mapping of app versions to heights. This allows for application logic to be
routed in the same binary, which enables single binary syncs and upgrades.

It accomplishes this by wrapping the `BaseApp`'s `EndBlock` method and checking
if the current height is equal to the height at which the app version should be
incremented. If it is, then the app version is incremented and the new version
is set in the header.

```go
func (app *App) EndBlock(req abci.RequestEndBlock) (res abci.ResponseEndBlock) {
	res = app.BaseApp.EndBlock(req)
	ctx := app.GetContextForDeliverTx([]byte{1})
	return appversion.EndBlocker(ctx, app.VersionKeeper, res)
}

// EndBlocker will modify the app version if the current height is equal to
// a predefined height at which the app version should be changed.
func EndBlocker(ctx sdk.Context, keeper Keeper, resp abci.ResponseEndBlock) abci.ResponseEndBlock {
	newAppVersion := keeper.GetVersion(ctx)
	if ctx.BlockHeader().Version.App != newAppVersion {
		resp.ConsensusParamUpdates.Version = &coretypes.VersionParams{
			AppVersion: newAppVersion,
		}
		// set the version in the application to ensure that tendermint is
		// passed the correct value upon rebooting
		keeper.versionSetter.SetProtocolVersion(newAppVersion)
	}
	return resp
}
```

## State

The state in the version module is abnormal because it can be modified by the
party building the binary without breaking the app hash. This is because the
state is not stored in the iavl tree, but rather as a simple predefined golang
map. It's important to note that even though this state is not in the state
store, that it can still cause consensus breaking changes. This is because that
state controls at which height the application will increment the app version.

```go
type Keeper struct {
	chainAppVersions map[string]ChainVersionConfig
}
```

## Events

TODO: Add events

### Usage

The standard usage of the version module is to load the app version height
mappings that are embedded into the binary, but it is also possible to load
custom mappings by including a `map[string]ChainVersionConfig` in the
app options using the `version.CustomVersionConfigKey` key.

Mappings are defined by a `map[uint64]int64` where the key is the app version
and the value is the height at which the app version should be incremented. For
example:

```go
vm := map[uint64]int64{
		1: 0,
		2: 112093,
		3: 300442,
		4: 420420,
}
```

The application will convert this mapping into a ChainVersionConfig:

```go
// HeightRange is a range of heights that a version is valid for. It is an
// internal struct used to search for the correct version given a height, and
// should only be created using the createRange function. Heights are
// non-overlapping and inclusive, meaning that the version is valid for all
// heights >= Start and <= End.
type HeightRange struct {
	Start   int64
	End     int64
	Version uint64
}

// ChainVersionConfig stores a set of version ranges and provides a method to
// retrieve the correct version for a given height.
type ChainVersionConfig struct {
	Ranges []HeightRange
}
```

which is used to determine the correct version for a given height. The keeper is
initiated with a mapping of these configs depending on the chain-id. This allows
for the chain-id to be changed and for multiple chain's (ie testnets) to be
supported.

```go
type Keeper struct {
	chainAppVersions map[string]ChainVersionConfig
}
```
