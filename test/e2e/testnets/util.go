package testnets

import (
	"io"
	"math/rand"
	"os"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

type keyGenerator struct {
	random *rand.Rand
}

func newKeyGenerator(seed int64) *keyGenerator {
	return &keyGenerator{
		random: rand.New(rand.NewSource(seed)), //nolint:gosec
	}
}

func (g *keyGenerator) Generate(keyType string) crypto.PrivKey {
	seed := make([]byte, ed25519.SeedSize)

	_, err := io.ReadFull(g.random, seed)
	if err != nil {
		panic(err) // this shouldn't happen
	}
	switch keyType {
	case "secp256k1":
		return secp256k1.GenPrivKeySecp256k1(seed)
	case "", "ed25519":
		return ed25519.GenPrivKeyFromSecret(seed)
	default:
		panic("KeyType not supported") // should not make it this far
	}
}

type GrafanaInfo struct {
	Endpoint string
	Username string
	Token    string
}

func GetGrafanaInfoFromEnvVar() *GrafanaInfo {
	if os.Getenv("GRAFANA_ENDPOINT") == "" ||
		os.Getenv("GRAFANA_USERNAME") == "" ||
		os.Getenv("GRAFANA_TOKEN") == "" {
		return nil
	}

	return &GrafanaInfo{
		Endpoint: os.Getenv("GRAFANA_ENDPOINT"),
		Username: os.Getenv("GRAFANA_USERNAME"),
		Token:    os.Getenv("GRAFANA_TOKEN"),
	}
}
