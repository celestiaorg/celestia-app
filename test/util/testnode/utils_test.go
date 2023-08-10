package testnode_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

func TestTestAccount(t *testing.T) {
	kr := testnode.TestKeyring()
	record, err := kr.Key(testnode.TestAccName)
	require.NoError(t, err)
	addr, err := record.GetAddress()
	require.NoError(t, err)
	fmt.Println(addr)
	require.Equal(t, testnode.TestAccAddr, addr.String())
	require.Equal(t, testnode.TestAddress(), addr)
}
