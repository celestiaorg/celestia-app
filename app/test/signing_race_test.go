package app_test

import (
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestSigningContextGetSignersConcurrent verifies that calling GetSigners on
// the signing context concurrently does not trigger a data race. Before the
// fix (adding a replace directive for cosmossdk.io/x/tx to use the patched
// fork), the closure returned by makeGetSignersFunc captured the outer
// function's err variable and concurrent callers would race on writing to it.
// This test reliably fails with `go test -race` against the unfixed
// cosmossdk.io/x/tx@v0.13.8.
func TestSigningContextGetSignersConcurrent(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cdc := enc.Codec.(*codec.ProtoCodec)

	from := testnode.RandomAddress()
	to := testnode.RandomAddress()
	msg := &banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			signers, _, err := cdc.GetMsgV1Signers(msg)
			require.NoError(t, err)
			require.Len(t, signers, 1)
		}()
	}
	wg.Wait()
}
