# `x/paramfilter`

## Abstract

The paramfilter module allows for specific parameters to be added to a block
list, so that they cannot be changed by governance proposals. This is useful for
forcing hardforks to change parameters that are critical to the network's
operation that are also part of the cosmos-sdk's standard modules. New modules
should not use this module, and instead use hardcoded constants.

## State

The state consists only of the parameters that are protected by the paramfilter.
All state is immutable and stored in memory during the application's
initialization.

```go
type Keeper struct {
	forbiddenParams map[string]bool
}

func NewKeeper(forbiddenParams ...[2]string) Keeper {
	consolidatedParams := make(map[string]bool, len(forbiddenParams))
	for _, param := range forbiddenParams {
		consolidatedParams[fmt.Sprintf("%s-%s", param[0], param[1])] = true
	}
	return Keeper{forbiddenParams: consolidatedParams}
}
```

## Usage

Pass a list of the forbidden subsapce key pairs that describe each parameter to
the keeper, then register the paramfilter handler with the governance module.

```go
func (*App) ForbiddenParams() [][2]string {
	return [][2]string{
		{banktypes.ModuleName, string(banktypes.KeySendEnabled)},
		{stakingtypes.ModuleName, string(stakingtypes.KeyUnbondingTime)},
		{stakingtypes.ModuleName, string(stakingtypes.KeyBondDenom)},
		{baseapp.Paramspace, string(baseapp.ParamStoreKeyValidatorParams)},
	}
}

func NewApp(...) *App {
    ...
    app.ParamFilterKeeper = paramfilter.NewKeeper(app.ForbiddenParams()...)
    ...
    govRouter.AddRoute(paramproposal.RouterKey, paramfilter.NewParamChangeProposalHandler(app.ParamFilterKeeper, app.ParamsKeeper))
    ...
}
```
