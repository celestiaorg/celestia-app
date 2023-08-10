package testfactory_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/stretchr/testify/require"
)

func TestTestAccount(t *testing.T) {
	kr := testfactory.TestKeyring()
	record, err := kr.Key(testfactory.TestAccName)
	require.NoError(t, err)
	addr, err := record.GetAddress()
	require.NoError(t, err)
	fmt.Println(addr)
	require.Equal(t, testfactory.TestAccAddr, addr.String())
	require.Equal(t, testfactory.TestAddress(), addr)
}
