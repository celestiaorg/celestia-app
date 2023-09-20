package network

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
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
	modifiedConfigs, err := setMnemomics(accounts, nodeConfigs)
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
