package testnet

import (
	"log"
)

func NoError(message string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", message, err)
	}
}

func AssertGreaterOrEqual(message string, value1, value2 int) {
	if value1 < value2 {
		log.Fatalf("%s: expected %d to be greater or equal to %d", message, value1, value2)
	}
}

func AssertLessOrEqual(message string, value1, value2 int) {
	if value1 > value2 {
		log.Fatalf("%s: expected %d to be less or equal to %d", message, value1, value2)
	}
}

func AssertEqual(message string, value1, value2 int) {
	if value1 != value2 {
		log.Fatalf("%s: expected %d to be equal to %d", message, value1, value2)
	}
}

func AssertNotEqual(message string, value1, value2 int) {
	if value1 == value2 {
		log.Fatalf("%s: expected %d to be not equal to %d", message, value1, value2)
	}
}

func AssertGreater(message string, value1, value2 int) {
	if value1 <= value2 {
		log.Fatalf("%s: expected %d to be greater than %d", message, value1, value2)
	}
}

func AssertLess(message string, value1, value2 int) {
	if value1 >= value2 {
		log.Fatalf("%s: expected %d to be less than %d", message, value1, value2)
	}
}
