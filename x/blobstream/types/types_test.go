package types_test

import (
	"bytes"
	mrand "math/rand"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestValsetPowerDiff(t *testing.T) {
	specs := map[string]struct {
		start types.BridgeValidators
		diff  types.BridgeValidators
		exp   sdk.Dec
	}{
		"no diff": {
			start: types.BridgeValidators{
				{Power: 1, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 2, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 3, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			diff: types.BridgeValidators{
				{Power: 1, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 2, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 3, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			exp: sdk.NewDecWithPrec(0, 1), // 0.0
		},
		"one": {
			start: types.BridgeValidators{
				{Power: 1073741823, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 1073741823, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 2147483646, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			diff: types.BridgeValidators{
				{Power: 858993459, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 858993459, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 2576980377, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			exp: sdk.NewDecWithPrec(2, 1), // 0.2
		},
		"real world": {
			start: types.BridgeValidators{
				{Power: 678509841, EvmAddress: "0x6db48cBBCeD754bDc760720e38E456144e83269b"},
				{Power: 671724742, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 685294939, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 671724742, EvmAddress: "0x0A7254b318dd742A3086882321C27779B4B642a6"},
				{Power: 671724742, EvmAddress: "0x454330deAaB759468065d08F2b3B0562caBe1dD1"},
				{Power: 617443955, EvmAddress: "0x3511A211A6759d48d107898302042d1301187BA9"},
				{Power: 6785098, EvmAddress: "0x37A0603dA2ff6377E5C7f75698dabA8EE4Ba97B8"},
				{Power: 291759231, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			diff: types.BridgeValidators{
				{Power: 642345266, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 678509841, EvmAddress: "0x6db48cBBCeD754bDc760720e38E456144e83269b"},
				{Power: 671724742, EvmAddress: "0x0A7254b318dd742A3086882321C27779B4B642a6"},
				{Power: 671724742, EvmAddress: "0x454330deAaB759468065d08F2b3B0562caBe1dD1"},
				{Power: 671724742, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 617443955, EvmAddress: "0x3511A211A6759d48d107898302042d1301187BA9"},
				{Power: 291759231, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
				{Power: 6785098, EvmAddress: "0x37A0603dA2ff6377E5C7f75698dabA8EE4Ba97B8"},
			},
			exp: sdk.NewDecWithPrec(10000000011641532, 18), // 0.010000000011641532
		},
	}

	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			startInternal, _ := spec.start.ToInternal()
			diffInternal, _ := spec.diff.ToInternal()
			obtained := startInternal.PowerDiff(*diffInternal)
			assert.True(t, spec.exp.Equal(obtained))
		})
	}
}

func TestValsetSort(t *testing.T) {
	specs := map[string]struct {
		src types.BridgeValidators
		exp types.BridgeValidators
	}{
		"by power desc": {
			src: types.BridgeValidators{
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(3)}, 20)).String()},
				{Power: 2, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(1)}, 20)).String()},
				{Power: 3, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(2)}, 20)).String()},
			},
			exp: types.BridgeValidators{
				{Power: 3, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(2)}, 20)).String()},
				{Power: 2, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(1)}, 20)).String()},
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(3)}, 20)).String()},
			},
		},
		"by eth addr on same power": {
			src: types.BridgeValidators{
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(2)}, 20)).String()},
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(1)}, 20)).String()},
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(3)}, 20)).String()},
			},
			exp: types.BridgeValidators{
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(1)}, 20)).String()},
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(2)}, 20)).String()},
				{Power: 1, EvmAddress: gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(3)}, 20)).String()},
			},
		},
		// if you're thinking about changing this due to a change in the sorting algorithm
		// you MUST go change this in gravity_utils/types.rs as well. You will also break all
		// bridges in production when they try to migrate so use extreme caution!
		"real world": {
			src: types.BridgeValidators{
				{Power: 678509841, EvmAddress: "0x6db48cBBCeD754bDc760720e38E456144e83269b"},
				{Power: 671724742, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 685294939, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 671724742, EvmAddress: "0x0A7254b318dd742A3086882321C27779B4B642a6"},
				{Power: 671724742, EvmAddress: "0x454330deAaB759468065d08F2b3B0562caBe1dD1"},
				{Power: 617443955, EvmAddress: "0x3511A211A6759d48d107898302042d1301187BA9"},
				{Power: 6785098, EvmAddress: "0x37A0603dA2ff6377E5C7f75698dabA8EE4Ba97B8"},
				{Power: 291759231, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
			},
			exp: types.BridgeValidators{
				{Power: 685294939, EvmAddress: "0x479FFc856Cdfa0f5D1AE6Fa61915b01351A7773D"},
				{Power: 678509841, EvmAddress: "0x6db48cBBCeD754bDc760720e38E456144e83269b"},
				{Power: 671724742, EvmAddress: "0x0A7254b318dd742A3086882321C27779B4B642a6"},
				{Power: 671724742, EvmAddress: "0x454330deAaB759468065d08F2b3B0562caBe1dD1"},
				{Power: 671724742, EvmAddress: "0x8E91960d704Df3fF24ECAb78AB9df1B5D9144140"},
				{Power: 617443955, EvmAddress: "0x3511A211A6759d48d107898302042d1301187BA9"},
				{Power: 291759231, EvmAddress: "0xF14879a175A2F1cEFC7c616f35b6d9c2b0Fd8326"},
				{Power: 6785098, EvmAddress: "0x37A0603dA2ff6377E5C7f75698dabA8EE4Ba97B8"},
			},
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			srcInternal, _ := spec.src.ToInternal()
			expInternal, _ := spec.exp.ToInternal()
			srcInternal.Sort()
			assert.Equal(t, srcInternal, expInternal)
			shuffled := shuffled(*srcInternal)
			shuffled.Sort()
			assert.Equal(t, shuffled, *expInternal)
		})
	}
}

func shuffled(v types.InternalBridgeValidators) types.InternalBridgeValidators {
	mrand.Shuffle(len(v), func(i, j int) {
		v[i], v[j] = v[j], v[i]
	})
	return v
}
