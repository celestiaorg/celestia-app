package main

import (
	"fmt"
	"log"
	"os"

	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
)

// This will only run tests within the v1 major release cycle
const (
	MajorVersion = v1.Version
	seed         = 42
)

var latestVersion = "latest"

func main() {
	logger := log.New(os.Stdout, "test", log.LstdFlags)
	// FIXME: This test currently panics in InitGenesis
	// it's currently not running
	if os.Getenv("RUN_MINOR_VERSION_COMPATIBILITY") == "true" {
		logger.Println("Running minor version compatibility test")
		err := MinorVersionCompatibility(logger)
		if err != nil {
			logger.Fatalf("Error running minor version compatibility: %v", err)
		}
	}

	logger.Println("====== Running major upgrade to v2 e2e test ======")
	// err := MajorUpgradeToV2(logger)
	// if err != nil {
	// 	logger.Fatalf("Error running minor version compatibility: %v", err)
	// }

	logger.Println("====== Running simple e2e test ======")
	err := E2ESimple(logger)
	if err != nil {
		logger.Fatalf("Error running simple e2e test: %v", err)
	}
}

// helper function to wrap errors
func NoError(message string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}
