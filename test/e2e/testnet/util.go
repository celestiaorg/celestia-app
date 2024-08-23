package testnet

import (
	"io"
	"math/rand"
	"os"

	"github.com/rs/zerolog/log"
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
	log.Info().Msg("Checking Grafana environment variables")
	if os.Getenv("GRAFANA_ENDPOINT") == "" ||
		os.Getenv("GRAFANA_USERNAME") == "" ||
		os.Getenv("GRAFANA_TOKEN") == "" {

		log.Info().Msg("No Grafana environment variables found")
		return nil
	}

	log.Info().Msg("Grafana environment variables found")
	return &GrafanaInfo{
		Endpoint: os.Getenv("GRAFANA_ENDPOINT"),
		Username: os.Getenv("GRAFANA_USERNAME"),
		Token:    os.Getenv("GRAFANA_TOKEN"),
	}
}

func equalOrHigher(v1, v2 string) bool {
	latest, err := GetLatestVersion()
	if err != nil && v1 == latest {
		return true
	}
	if v1 >= v2 {
		print(v1, v2, "equalOrHigher", true)
		return true
	}
	print(v1, v2, "equalOrHigher", false)
	return false

}
