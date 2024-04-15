package main

import (
	"errors"
	"log"
	"os"

	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
)

// This will only run tests within the v1 major release cycle
const (
	MajorVersion = v1.Version
	seed         = 42
)

var (
	latestVersion = "latest"
	ErrSkip       = errors.New("skipping e2e test")
)

type TestFunc func(*log.Logger) error

type Test struct {
	Name string
	Func TestFunc
}

func main() {
	logger := log.New(os.Stdout, "test-e2e", log.LstdFlags)

	tests := []Test{
		{"MinorVersionCompatibility", MinorVersionCompatibility},
		{"MajorUpgradeToV2", MajorUpgradeToV2},
		{"E2ESimple", E2ESimple},
	}

	testName := os.Getenv("TEST")

	if testName != "" {
		for _, test := range tests {
			if test.Name == testName {
				runTest(logger, test)
				return
			}
		}
		logger.Fatalf("Unknown test: %s", testName)
	} else {
		for _, test := range tests {
			runTest(logger, test)
		}
	}
}

func runTest(logger *log.Logger, test Test) {
	logger.SetPrefix("             ")
	logger.Printf("=== RUN %s", test.Name)
	err := test.Func(logger)
	if err != nil {
		if errors.Is(err, ErrSkip) {
			logger.Printf("--- SKIPPING: %s. Reason: %v \n\n", test.Name, err)
			return
		}
		logger.Fatalf("--- ERROR %s: %v", test.Name, err)
	}
	logger.Printf("--- ✅ PASS: %s \n\n", test.Name)
}
