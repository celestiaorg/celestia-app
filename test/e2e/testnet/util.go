package testnet

import (
	"log"
	"os"
)

type GrafanaInfo struct {
	Endpoint string
	Username string
	Token    string
}

func GetGrafanaInfoFromEnvVar(logger *log.Logger) *GrafanaInfo {
	logger.Println("Checking Grafana environment variables")
	if os.Getenv("GRAFANA_ENDPOINT") == "" ||
		os.Getenv("GRAFANA_USERNAME") == "" ||
		os.Getenv("GRAFANA_TOKEN") == "" {

		logger.Println("No Grafana environment variables found")
		return nil
	}

	logger.Println("Grafana environment variables found")
	return &GrafanaInfo{
		Endpoint: os.Getenv("GRAFANA_ENDPOINT"),
		Username: os.Getenv("GRAFANA_USERNAME"),
		Token:    os.Getenv("GRAFANA_TOKEN"),
	}
}
