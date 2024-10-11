package main

import (
	"log"
	"os"
	"strings"
)

func main() {
	logger := log.New(os.Stdout, "test-e2e-benchmark", log.LstdFlags)

	tests := []Test{
		{"TwoNodeSimple", TwoNodeSimple},
		{"TwoNodeBigBlock8MB", TwoNodeBigBlock8MB},
		{"TwoNodeBigBlock8MBLatency", TwoNodeBigBlock8MBLatency},
		{"LargeNetworkBigBlock8MB", LargeNetworkBigBlock8MB},
	}

	// check the test name passed as an argument and run it
	if len(os.Args) < 2 {
		logger.Println("No test is specified.")
		logger.Println("Usage: go run ./test/e2e/benchmark <test_name>")
		logger.Printf("Valid tests are: %s\n\n", getTestNames(tests))
		return

	}
	found := false
	testName := os.Args[1]
	for _, test := range tests {
		if test.Name == testName {
			found = true
			runTest(logger, test)
			break
		}
	}
	if !found {
		logger.Printf("Invalid test name: %s\n", testName)
		logger.Printf("Valid tests are: %s\n", getTestNames(tests))
		logger.Println("Usage: go run ./test/e2e/benchmark <test_name>")

	}
}

type TestFunc func(*log.Logger) error

type Test struct {
	Name string
	Func TestFunc
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
