package network

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/genesis"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/influxdata/influxdb/pkg/testing/assert"
	"github.com/stretchr/testify/require"
	// Replace with the imports where your types are defined
	// "your_package/tmconfig"
	// "your_package/srvconfig"
)

func TestSetMnemonics(t *testing.T) {
	// Initialize example data
	accounts := []genesis.Account{
		{Name: "Alice", Mnemonic: "AliceMnemonic"},
		{Name: "Bob", Mnemonic: "BobMnemonic"},
	}

	nodeConfigs := []NodeConfig{
		{
			Name: "Alice",
			Keys: KeySet{},
		},
		{
			Name: "Bob",
			Keys: KeySet{},
		},
	}

	// Test setting mnemonics
	modifiedConfigs, err := setMnenomics(accounts, nodeConfigs)
	require.NoError(t, err)

	for _, cfg := range modifiedConfigs {
		require.NotEmpty(t, cfg.Keys.AccountMnemonic)
	}
}

func TestConfigGeneration(t *testing.T) {
	p := Params{
		Validators:  3,
		FullNodes:   0,
		Timeout:     0,
		TopologyFns: []TopologyFn{ConnectAll},
		HaltHeight:  100,
		Pex:         false,
	}

	ss := []Status{
		{
			IP:             "10.32.0.21",
			GlobalSequence: 1,
			GroupSequence:  1,
			Group:          "validators",
			NodeType:       "validators",
		},
		{
			IP:             "10.45.0.19",
			GlobalSequence: 2,
			GroupSequence:  2,
			Group:          "validators",
			NodeType:       "validators",
		},
		{
			IP:             "10.44.128.11",
			GlobalSequence: 3,
			GroupSequence:  3,
			Group:          "validators",
			NodeType:       "validators",
		},
	}
	cfg, err := p.StandardConfig(ss)
	require.NoError(t, err)

	require.Equal(t, 3, len(cfg.Nodes))
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, node := range cfg.Nodes {
		require.NotEmpty(t, node.Keys.NetworkKey)
		require.NotEmpty(t, node.Keys.ConsensusKey)
		require.NotEmpty(t, node.Keys.AccountMnemonic)
		require.Equal(t, "validators", node.NodeType)

		kr := keyring.NewInMemory(ecfg.Codec)

		kr, err = ImportKey(kr, node.Keys.AccountMnemonic, node.Name)
		require.NoError(t, err)
		rec, err := kr.Key(node.Name)
		require.NoError(t, err)
		_, err = rec.GetAddress()
		require.NoError(t, err)
	}
}

func TestExportImportKey(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(ecfg.Codec)
	_, mn, err := kr.NewMnemonic("test", keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)
	rec, err := kr.Key("test")
	require.NoError(t, err)
	err = kr.Delete("test")
	require.NoError(t, err)
	rec, err = kr.NewAccount("test", mn, "", "", hd.Secp256k1)
	require.NoError(t, err)
	_, err = rec.GetAddress()
	require.NoError(t, err)

	_, _, err = kr.NewMnemonic("test-2", keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)
	rec, err = kr.Key("test-2")
	require.NoError(t, err)
	armor, err := kr.ExportPrivKeyArmor("test-2", "")
	require.NoError(t, err)
	err = kr.Delete("test-2")
	require.NoError(t, err)
	err = kr.ImportPrivKey("test-2", armor, "")
	assert.NoError(t, err)
	rec, err = kr.Key("test-2")
	require.NoError(t, err)
}

func TestImportArmor(t *testing.T) {
	armor := `
-----BEGIN TENDERMINT PRIVATE KEY-----
kdf: bcrypt
salt: 18156479C183D89BFD7D6AC648719FB4
type: secp256k1

EsoSHTVeSmuv7r5jEeXy7a+iqgnLYkijUrSLJljQTWP9jG9U5YBjL4dUlcSxku36
iigF+ApHTckpsrZNDcy12wgzsjIblDbPjs3nJHY=
=LSQ2
-----END TENDERMINT PRIVATE KEY-----
`
	// 	armor = `
	// -----BEGIN TENDERMINT PRIVATE KEY-----
	// kdf: bcrypt
	// salt: 4978B7888973C9531B3E1DF1DA0AA1AA
	// type: secp256k1

	// cJ8CcBCA93SSbKiAUB7T0zCkHWjxPw7Aa3hnczZL5xmuP6LPOU9j0YTeov2PY5S1
	// 8Rhk0604OAtJF2uJzK8aUOaJydsAcsS4H8BSbM4=
	// =a8km
	// -----END TENDERMINT PRIVATE KEY-----
	// `
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(ecfg.Codec)
	err := kr.ImportPrivKey("test-2", armor, "")
	assert.NoError(t, err)
	_, err = kr.Key("test-2")
	require.NoError(t, err)

}

func TestImportMnenomic(t *testing.T) {
	mn := "wife other cry crucial other clog bright seven husband ugly uncle sing layer glad silly keen hour custom firm review erosion lift mobile together"
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(ecfg.Codec)
	_, err := kr.NewAccount("test", mn, "", "", hd.Secp256k1)
	require.NoError(t, err)
	_, err = kr.Key("test")
	require.NoError(t, err)

}

func TestTTT(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(ecfg.Codec)
	_, mn, err := kr.NewMnemonic("test", keyring.English, "", "", hd.Secp256k1)
	require.NoError(t, err)
	rec, err := kr.Key("test")
	require.NoError(t, err)

	r := strings.NewReader(mn)
	bz, err := io.ReadAll(r)
	require.NoError(t, err)

	kr2 := keyring.NewInMemory(ecfg.Codec)
	rec, err = kr2.NewAccount("test", fmt.Sprintf("%s", string(bz)), "", "", hd.Secp256k1)
	require.NoError(t, err)
	_, err = rec.GetAddress()
	require.NoError(t, err)

	manual := "wife other cry crucial other clog bright seven husband ugly uncle sing layer glad silly keen hour custom firm review erosion lift mobile together"
	kr3 := keyring.NewInMemory(ecfg.Codec)
	rec, err = kr3.NewAccount("test", manual, "", "", hd.Secp256k1)
	require.NoError(t, err)
	_, err = rec.GetAddress()
	require.NoError(t, err)
}
