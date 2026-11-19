package app

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestValidateRecheck(t *testing.T) {
	a, bank := storetypes.NewKVStoreKey("a"), storetypes.NewKVStoreKey(banktypes.StoreKey)
	read := func(g int, k string) keyAccess { return keyAccess{storeKey: storeKey{a, k}, group: g} }
	write := func(g int, k string, v []byte) keyAccess {
		return keyAccess{storeKey: storeKey{a, k}, group: g, value: v}
	}
	fee := func(g int, v string) keyAccess {
		return keyAccess{storeKey: storeKey{bank, feeCollectorBalanceKey}, group: g, value: []byte(v)}
	}
	for _, tc := range []struct {
		name string
		log  recheckLog
		ok   bool
	}{
		{"own read and write", recheckLog{reads: []keyAccess{read(0, "k")}, writes: []keyAccess{write(0, "k", []byte("1"))}}, true},
		{"read of another group's write", recheckLog{reads: []keyAccess{read(1, "k")}, writes: []keyAccess{write(0, "k", []byte("1"))}}, false},
		{"different writes", recheckLog{writes: []keyAccess{write(0, "k", []byte("1")), write(1, "k", []byte("2"))}}, false},
		{"write and delete", recheckLog{writes: []keyAccess{write(0, "k", []byte{}), write(1, "k", nil)}}, false},
		{"equal writes", recheckLog{writes: []keyAccess{write(0, "k", []byte{}), write(1, "k", []byte{})}}, true},
		{"read of an equally written key", recheckLog{reads: []keyAccess{read(0, "k")}, writes: []keyAccess{write(0, "k", []byte{}), write(1, "k", []byte{})}}, false},
		{"range over another group's write", recheckLog{ranges: []rangeAccess{{a, 1, []byte("j"), []byte("l")}}, writes: []keyAccess{write(0, "k", []byte("1"))}}, false},
		{"range past another group's write", recheckLog{ranges: []rangeAccess{{a, 1, []byte("l"), nil}}, writes: []keyAccess{write(0, "k", []byte("1"))}}, true},
		{"range over own write", recheckLog{ranges: []rangeAccess{{a, 0, nil, nil}}, writes: []keyAccess{write(0, "k", []byte("1"))}}, true},
		{"fee collector credits", recheckLog{writes: []keyAccess{fee(0, "5"), fee(1, "7")}}, true},
		{"range over fee collector credits", recheckLog{ranges: []rangeAccess{{bank, 0, nil, nil}}, writes: []keyAccess{fee(0, "5"), fee(1, "7")}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRecheck([]*recheckLog{&tc.log})
			require.Equal(t, tc.ok, err == nil, "%v", err)
		})
	}
}
