package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	talisClient "github.com/celestiaorg/talis/pkg/api/v1/client"
	"github.com/celestiaorg/talis/pkg/api/v1/handlers"
	"github.com/celestiaorg/talis/pkg/db/models"
)

type Validator struct {
	IP     string `json:"ip"`
	Slug   string `json:"slug"`
	Region string `json:"region"`
	Name   string `json:"name"`
}

// Config describes the desired state of the network.
type Config struct {
	Validators     []Validator `json:"validators"`
	IP             string      `json:"ip"`
	Key            string      `json:"key"`
	ChainID        string      `json:"chain_id"`
	Project        string      `json:"project"`
	UserID         int         `json:"user"`
	UserSSHKeyName string      `json:"user_ssh_key_name"`
}

func NewTestConfig() Config {
	vals := make([]Validator, 0, len(DOSmallRegions))
	for region, count := range DOSmallRegions {
		for i := 0; i < count; i++ {
			vals = append(vals, Validator{
				IP:     "this will update after node is spun up",
				Name:   "this will update after node is spun up",
				Slug:   DODefaultValidatorSlug,
				Region: region,
			})
		}

	}
	return Config{
		Validators: vals,
	}
}

func NewConfig(vals int) Config {
	// Create a new config with the specified number of validators
	validators := make([]Validator, vals)
	for i := 0; i < vals; i++ {
		validators[i] = Validator{
			IP:     "this will update after node is spun up",
			Name:   "this will update after node is spun up",
			Slug:   DODefaultValidatorSlug,
			Region: RandomDORegion(),
		}
	}
	return Config{
		Validators: validators,
	}
}

func (c Config) Save(root string) error {
	// Create the directory if it doesn't exist
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}

	// Create the config file path
	configFilePath := filepath.Join(root, "config.json")

	cfgFile, err := os.OpenFile(configFilePath, os.O_RDWR|os.O_CREATE|os.O_SYNC, 0755)
	if err != nil {
		return err
	}
	defer cfgFile.Close()

	// Write the config to the file
	encoder := json.NewEncoder(cfgFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

// LoadConfig loads the config from the specified path.
func LoadConfig(path string) (Config, error) {
	cfgFile, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer cfgFile.Close()

	var cfg Config
	decoder := json.NewDecoder(cfgFile)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// State describes the current state of the network.
type State struct {
	Validators []Validator `json:"validators"`
}

func (cfg Config) Options(timeout time.Duration) *talisClient.Options {
	return &talisClient.Options{
		BaseURL: cfg.IP,
		APIKey:  cfg.Key,
		Timeout: timeout,
	}
}

func (cfg Config) TalisClient(timeout time.Duration) (talisClient.Client, error) {
	opts := cfg.Options(timeout)
	return talisClient.NewClient(opts)
}

func PingTalisServer(ip, key string) error {
	opts := &talisClient.Options{
		BaseURL: ip,
		APIKey:  key,
		Timeout: time.Second * 5,
	}

	apiClient, err := talisClient.NewClient(opts)
	if err != nil {
		log.Fatalf("Error creating API client: %v", err)
	}

	// You can now use apiClient to interact with the Talis API
	// Example: Perform a health check
	health, err := apiClient.HealthCheck(context.Background())
	if err != nil {
		log.Fatalf("Health check failed: %v", err)
	}

	if health["status"] != "healthy" {
		return fmt.Errorf("Talis server is down: %v", health)
	}

	return nil
}

func OpenProject(cfg Config, project string) (models.Project, error) {
	opts := cfg.Options(5 * time.Second)

	apiClient, err := talisClient.NewClient(opts)
	if err != nil {
		log.Fatalf("Error creating API client: %v", err)
	}

	pp := handlers.ProjectCreateParams{
		Name:        project,
		Description: "celestia-app",
		OwnerID:     uint(cfg.UserID),
	}

	p, err := apiClient.CreateProject(context.Background(), pp)

	return p, err
}
