package gasmonitor

import (
	"encoding/json"
	"fmt"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	AppOptionsKey = "GasMonitor"
)

var _ sdk.AnteDecorator = &Decorator{}

// AnteHandle replaces the gas meter with the provided context with one that
// monitors the amount of gas consumed it fulfills the ante.Decorator interface.
type Decorator struct {
	Monitors []*MonitoredGasMeter
}

// NewDecorator returns a new decorator that can be used to monitor gas usage.
func NewDecorator() *Decorator {
	return &Decorator{}
}

// NewMonitoredGasMeter wraps the provided context's gas meter with a monitored
// version.
func NewMonitoredGasMeter(ctx sdk.Context) (*MonitoredGasMeter, sdk.Context) {
	meter := ctx.GasMeter()
	monitor := &MonitoredGasMeter{
		GasMeter: meter,
		Hash:     tmbytes.HexBytes(coretypes.Tx(ctx.TxBytes()).Hash()).String(),
		Height:   ctx.BlockHeight(),
	}
	return monitor, ctx.WithGasMeter(monitor)
}

var _ sdk.GasMeter = &MonitoredGasMeter{}

// MonitoredGasMeter wraps any sdk.GasMeter to record the amount of gas consumed
// or refunded
type MonitoredGasMeter struct {
	sdk.GasMeter
	Name     string            `json:"name"`
	Hash     string            `json:"hash"`
	Height   int64             `json:"height"`
	Readings []Reading         `json:"readings"`
	Summary  map[string]uint64 `json:"summary"`
	Total    uint64            `json:"total"`
}

func (gm *MonitoredGasMeter) Summarize() {
	summary := make(map[string]uint64)
	for _, r := range gm.Readings {
		summary[r.Description] += r.Amount
	}
	gm.Summary = summary
	gm.Total = gm.GasConsumed()
}

func SaveJSON(path string, data map[string]*MonitoredGasMeter) error {
	file, err := os.OpenFile(fmt.Sprintf("%s.json", path), os.O_CREATE|os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(data)
}

// Reading represents a single instance of gas consumption or refunding
type Reading struct {
	Description string  `json:"description"`
	Refund      bool    `json:"refund"`
	Amount      sdk.Gas `json:"amount"`
}

// ConsumeGas wraps the underlying ConsumeGas method to record the amount of gas
// consumed.
func (gm *MonitoredGasMeter) ConsumeGas(amount sdk.Gas, descriptor string) {
	gm.Readings = append(gm.Readings, Reading{Description: descriptor, Refund: false, Amount: amount})
	gm.GasMeter.ConsumeGas(amount, descriptor)
}

// ConsumeGas wraps the underlying RefundGas method to record the amount of gas
// refunded.
func (gm *MonitoredGasMeter) RefundGas(amount sdk.Gas, descriptor string) {
	gm.Readings = append(gm.Readings, Reading{Description: descriptor, Refund: true, Amount: amount})
	gm.GasMeter.RefundGas(amount, descriptor)
}

// AnteHandle replaces the gas meter with the provided context with one that
// monitors the amount of gas consumed it fulfills the ante.Decorator interface.
func (h *Decorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return next(ctx, tx, simulate)
	}
	meter, ctx := NewMonitoredGasMeter(ctx)
	h.Monitors = append(h.Monitors, meter)
	return next(ctx, tx, simulate)
}
