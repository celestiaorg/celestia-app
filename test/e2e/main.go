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

type TestFunc func(*log.Logger) error

type Test struct {
	Name string
	Func TestFunc
}

func main() {
	log.SetFlags(0) // Disable additional information like the date or time
	log.SetPrefix("    ")

	logger := log.New(os.Stdout, "test-e2e", log.LstdFlags)

	tests := []Test{
		{"MinorVersionCompatibility", MinorVersionCompatibility},
		{"MajorUpgradeToV2", MajorUpgradeToV2},
		{"E2ESimple", E2ESimple},
	}

	testName := os.Getenv("TEST")

	if testName != "" {
		for _, test := range tests {
			fmt.Println(test.Name)
			fmt.Println(testName)
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
	logger.Printf("=== RUN %s", test.Name)
	err := test.Func(logger)
	if err != nil {
		logger.Fatalf("--- ERROR %s: %v", test.Name, err)
	}
	logger.Printf("--- ✅ PASS: %s", test.Name)
}
