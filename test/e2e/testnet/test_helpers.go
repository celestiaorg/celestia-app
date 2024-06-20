package testnet

import (
	"log"
)

func NoError(message string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}
