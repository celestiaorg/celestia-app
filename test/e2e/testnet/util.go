package testnet

import (
	"os"

	"github.com/rs/zerolog/log"
)

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
