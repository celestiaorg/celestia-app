package gasmonitor

import (
	"encoding/json"
	"fmt"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	AppOptionsKey = "GasMonitor"
)

var _ sdk.AnteDecorator = &Decorator{}

// AnteHandle replaces the gas meter with the provided context with one that
// monitors the amount of gas consumed it fulfills the ante.Decorator interface.
type Decorator struct {
	Traces []*GasConsumptionTrace
}

// NewDecorator returns a new decorator that can be used to monitor gas usage.
func NewDecorator() *Decorator {
	return &Decorator{}
}

func (d *Decorator) SaveJSON() error {
	if len(d.Traces) == 0 {
		return nil
	}

	path := fmt.Sprintf("gas_consumption_trace_%s.json", tmrand.Str(6))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}

	defer file.Close()
	return json.NewEncoder(file).Encode(d.Traces)
}

// NewMonitoredGasMeter wraps the provided context's gas meter with a monitored
// version.
func NewGasConsumptionTrace(ctx sdk.Context, tx sdk.Tx) (*GasConsumptionTrace, sdk.Context) {
	meter := ctx.GasMeter()

	msgs := tx.GetMsgs()
	msgNames := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		msgNames = append(msgNames, sdk.MsgTypeURL(msg))
	}

	trace := &GasConsumptionTrace{
		GasMeter: meter,
		Hash:     tmbytes.HexBytes(coretypes.Tx(ctx.TxBytes()).Hash()).String(),
		Height:   ctx.BlockHeight(),
		MsgNames: msgNames,
	}

	return trace, ctx.WithGasMeter(trace)
}

var _ sdk.GasMeter = &GasConsumptionTrace{}

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

// Summarize records all metadata using already recorded data.
func (gm *GasConsumptionTrace) Summarize() {
	gm.Total = gm.GasConsumed()
}

// Reading represents a single instance of gas consumption or refunding
type Reading struct {
	Description string  `json:"description"`
	Refund      bool    `json:"refund"`
	Amount      sdk.Gas `json:"amount"`
}

// ConsumeGas wraps the underlying ConsumeGas method to record the amount of gas
// consumed.
func (gm *GasConsumptionTrace) ConsumeGas(amount sdk.Gas, descriptor string) {
	gm.Readings = append(gm.Readings, Reading{Description: descriptor, Refund: false, Amount: amount})
	gm.GasMeter.ConsumeGas(amount, descriptor)
}

// ConsumeGas wraps the underlying RefundGas method to record the amount of gas
// refunded.
func (gm *GasConsumptionTrace) RefundGas(amount sdk.Gas, descriptor string) {
	gm.Readings = append(gm.Readings, Reading{Description: descriptor, Refund: true, Amount: amount})
	gm.GasMeter.RefundGas(amount, descriptor)
}

// AnteHandle replaces the gas meter with the provided context with one that
// monitors the amount of gas consumed it fulfills the ante.Decorator interface.
func (h *Decorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	trace, ctx := NewGasConsumptionTrace(ctx, tx)
	h.Traces = append(h.Traces, trace)
	return next(ctx, tx, simulate)
}
