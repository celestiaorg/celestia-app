package main

import (
	"log"
	"os"
	"strings"
)

const (
	timeFormat = "20060102_150405"
)

type TestFunc func(logger *log.Logger) error

type Test struct {
	Name string
	Func TestFunc
}

const (
	// TestConfigKey is the key used to retrieve which test is being ran from
	// the pulumi config. This value can be set by running `pulumi config set test TestName`
	TestConfigKey = "test"

	// RegionsConfgKey is the key used to retrieve the regions that the network
	// should be deployed to from the pulumi config. This value can be set by
	// running `pulumi config set regions Full`.
	RegionsConfgKey = "regions"

	// ChainIDConfigKey is the key used to retrieve the chain ID that the network
	// should be deployed with from the pulumi config. This value can be set by
	// running `pulumi config set chainID ChainID`.
	ChainIDConfigKey = "chainID"

	// GlobalTimeout is passed to all pulumi resources to ensure that they do
	// not stay alive too long.
	GlobalTimeoutString = "30m"
)

func main() {
	logger := log.New(os.Stdout, "test-e2e", log.LstdFlags)

	tests := []Test{
		{"MinorVersionCompatibility", MinorVersionCompatibility},
		{"MajorUpgradeToV2", MajorUpgradeToV2},
		{"MajorUpgradeToV3", MajorUpgradeToV3},
		{"E2ESimple", E2ESimple},
		{"MultiRegionTest", MultiRegionTest},
	}

	// check if a specific test is passed and run it
	specificTestFound := false
	for _, arg := range os.Args[1:] {
		for _, test := range tests {
			if test.Name == arg {
				runTest(logger, test)
				specificTestFound = true
				break
			}
		}
	}

	if !specificTestFound {
		logger.Println("No particular test specified. Running all tests.")
		logger.Println("make test-e2e <test_name> to run a specific test")
		logger.Printf("Valid tests are: %s\n\n", getTestNames(tests))
		// if no specific test is passed, run all tests
		for _, test := range tests {
			runTest(logger, test)
		}
	}

	return
}

func runTest(logger *log.Logger, test Test) {
	logger.Printf("=== RUN %s", test.Name)
	err := test.Func(logger)
	if err != nil {
		logger.Fatalf("--- ERROR %s: %v", test.Name, err)
	}
	logger.Printf("--- ✅ PASS: %s \n\n", test.Name)
}

func getTestNames(tests []Test) string {
	testNames := make([]string, 0, len(tests))
	for _, test := range tests {
		testNames = append(testNames, test.Name)
	}
	return strings.Join(testNames, ", ")
}
