# Gas Monitor

The `gasmonitor` package contains tools for capturing trace level information on
when and how gas is consumed. This functionality is only meant to be used during
limited tests, and never while running a node IRL. This is because the
`gasmonitor` will keep gas consumption traces in memory continually.

## Usage

### Starting Traces

When creating a `testnode`, set the gas consumption trace decorator as an
`appOption` with the specific key.

```go
	cfg := testnode.DefaultConfig()
	dec := gasmonitor.NewDecorator()
	// store the gas monitor to read from after execution
	s.gasMonitor = dec
	cfg.AppOptions.Set(gasmonitor.AppOptionsKey, dec)

### Saving Traces

After starting the network and executing transaction, call the `dec.SaveJSON()`
method to save the traces to a single file.

```go
	t.Cleanup(func() {
		err := dec.SaveJSON()
		require.NoError(t, err)
	})
```

### Schema

The `gasmonitor` will save a `[]GasConsumptionTrace` for each block height with
transactions in this format.

```go
// GasConsumptionTrace wraps any sdk.GasMeter to record the amount of gas consumed
// or refunded for a set of transactions in a block.
type GasConsumptionTrace struct {
	sdk.GasMeter `json:"-"`
	// MsgNames is a list of the names of the messages in the transaction
	MsgNames []string `json:"msg_names"`
	Hash     string   `json:"hash"`
	Height   int64    `json:"height"`
	// Readings is a list of the gas readings that occurred during the execution
	// of the transaction for this trace.
	Readings []Reading `json:"readings"`
	Total    uint64    `json:"total"`
}

// Reading represents a single instance of gas consumption or refunding
type Reading struct {
	Description string  `json:"description"`
	Refund      bool    `json:"refund"`
	Amount      sdk.Gas `json:"amount"`
}
```

## Plotting

While not maintained, [this script](https://gist.github.com/evan-forbes/948c8cf574f2f50b101c89a95ee1d43c) is a good starting point for plotting
the data collected from a gas trace using python.
