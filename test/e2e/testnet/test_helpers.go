package testnet

import (
	"fmt"
	"log"
)

func NoError(message string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}

type JSONRPCError struct {
	Code    int
	Message string
	Data    string
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSONRPC Error - Code: %d, Message: %s, Data: %s", e.Code, e.Message, e.Data)
}
