package genesis_test

import (
	"regexp"
	"testing"

	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// withPubKey replaces the validator's pub_key entry, including its trailing comma.
func (f fixture) withPubKey(t *testing.T, repl string) []byte {
	t.Helper()
	re := regexp.MustCompile(`"pub_key":\{"type":"tendermint/PubKeyEd25519","value":"[^"]*"\},`)
	require.Len(t, re.FindAll(f.file, -1), 1)
	return re.ReplaceAllLiteral(f.file, []byte(repl))
}

func TestValidatorsRejects(t *testing.T) {
	f := newFixture(t)
	key2 := ed25519.GenPrivKeyFromSecret([]byte("second")).PubKey()
	half := cmttypes.MaxTotalVotingPower/2 + 1

	runLoadCases(t, f, []loadCase{
		{
			name: "missing pub_key",
			file: func(t *testing.T) []byte { return f.withPubKey(t, "") },
			want: inputErr("consensus.validators[0].pub_key", "a valid public key"),
			got:  "not supported",
		},
		{
			name: "short ed25519 pub_key",
			file: func(t *testing.T) []byte {
				return f.withPubKey(t, `"pub_key":{"type":"tendermint/PubKeyEd25519","value":"AQ=="},`)
			},
			want: inputErr("consensus.validators[0].pub_key", "a valid public key"),
			got:  "invalid size",
		},
		{
			name: "pub_key type not allowed",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) {
				g.Consensus.Validators[0].PubKey = secp256k1.GenPrivKeySecp256k1([]byte("x")).PubKey()
			},
			want: inputErr("consensus.validators[0].pub_key", "one of [ed25519]"),
			got:  "secp256k1",
		},
		{
			name: "address not derived from pub_key",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Validators[0].Address = make([]byte, 20) },
			want: inputErr("consensus.validators[0].address", f.decode(t).Consensus.Validators[0].PubKey.Address().String()),
		},
		{
			name: "duplicate validator",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) {
				g.Consensus.Validators = append(g.Consensus.Validators, g.Consensus.Validators[0])
			},
			want: inputErr("consensus.validators", "a valid validator set"),
			got:  "duplicate entry",
		},
		{
			name: "total power above the cap",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) {
				g.Consensus.Validators[0].Power = half
				g.Consensus.Validators = append(g.Consensus.Validators, cmttypes.GenesisValidator{PubKey: key2, Power: half})
			},
			want: inputErr("consensus.validators", "a valid validator set"),
			got:  "total voting power",
		},
	})
}
