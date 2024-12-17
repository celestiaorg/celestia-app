package testnet

import (
	"log"
)

func NoError(message string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}

func NoErrorWithCleanup(message string, err error, cleanup func()) {
	if err != nil {
		log.Printf("%s: %v", message, err)
		cleanup()
		log.Fatalf("%s: %v", message, err)
	}
}
