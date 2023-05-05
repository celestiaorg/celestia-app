# `x/paramfilter`

## Abstract

The paramfilter module allows for specific parameters to be added to a block
list, so that they cannot be changed by governance proposals. If a proposal
contains a single blocked parameter change, then none of the parameters are
updated.

This is useful for forcing hardforks to change parameters that are
critical to the network's operation that are also part of the cosmos-sdk's
standard modules. New modules should not use this module, and instead use
hardcoded constants.

## State

The state consists only of the parameters that are protected by the paramfilter.
All state is immutable and stored in memory during the application's
initialization.

```go
// ParamBlockList keeps track of parameters that cannot be changed by governance
// proposals
type ParamBlockList struct {
	params map[string]bool
}

// NewParamBlockList creates a new ParamBlockList that can be used to block gov
// proposals that attempt to change hard-coded parameters.
func NewParamBlockList(blockedParams ...[2]string) ParamBlockList {
	consolidatedParams := make(map[string]bool, len(blockedParams))
	for _, param := range blockedParams {
		consolidatedParams[fmt.Sprintf("%s-%s", param[0], param[1])] = true
	}
	return ParamBlockList{params: consolidatedParams}
}
```

## Usage

Pass a list of the blocked subspace key pairs that describe each parameter to
the block list, then register the param change handler with the governance
module.

```go
func (*App) Blocked() [][2]string {
	return [][2]string{
		{banktypes.ModuleName, string(banktypes.KeySendEnabled)},
		{stakingtypes.ModuleName, string(stakingtypes.KeyUnbondingTime)},
		{stakingtypes.ModuleName, string(stakingtypes.KeyBondDenom)},
		{baseapp.Paramspace, string(baseapp.ParamStoreKeyValidatorParams)},
	}
}

func NewApp(...) *App {
    ...
    paramBlockList := paramfilter.NewParamBlockList(app.BlockedParams()...)

	// register the proposal types
	govRouter := oldgovtypes.NewRouter()
	govRouter.AddRoute(paramproposal.RouterKey, paramBlockList.GovHandler(app.ParamsKeeper))
    ...
}
```
