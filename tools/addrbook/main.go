package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"
)

const (
	// bucketTypeNew is the byte value CometBFT uses to represent a new bucket.
	//
	// Ref: https://github.com/celestiaorg/celestia-core/blob/f7635ef65de901906b4f63aa9cc7ac9fbd7d5223/p2p/pex/addrbook.go#L29
	bucketTypeNew = 0x01

	// inputFile is the filename of the input file containing the list of peers.
	inputFile = "peers.txt"

	// outputFile is the filename of the output file that will be generated.
	outputFile = "addrbook.json"

	// key is a hard-coded key for the address book.
	key = "075f251a11c6b2cef94f3d61"
)

type Address struct {
	ID   string `json:"id"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type Entry struct {
	Addr        Address   `json:"addr"`
	Src         Address   `json:"src"`
	Buckets     []int     `json:"buckets"`
	Attempts    int32     `json:"attempts"`
	BucketType  byte      `json:"bucket_type"`
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success"`
	LastBanTime time.Time `json:"last_ban_time"`
}

type Output struct {
	Key   string  `json:"key"`
	Addrs []Entry `json:"addrs"`
}

func main() {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		panic(err)
	}

	lines := strings.Split(string(data), "\n")

	// var addrs []Entry
	addrs := make([]Entry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "@")
		id := parts[0]
		domainAndPort := strings.Split(parts[1], ":")
		domain := domainAndPort[0]
		port := domainAndPort[1]

		ip, err := resolveDomain(domain)
		if err != nil {
			panic(err)
		}

		addr := Address{
			ID:   id,
			IP:   ip,
			Port: stringToInt(port),
		}

		entry := Entry{
			Addr:       addr,
			Src:        addr,
			Buckets:    []int{randBucketIndex()},
			BucketType: bucketTypeNew,
		}

		addrs = append(addrs, entry)
	}

	output := Output{
		Key:   key,
		Addrs: addrs,
	}

	jsonData, err := json.MarshalIndent(output, "", "\t")
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(outputFile, jsonData, 0o644); err != nil {
		panic(err)
	}

	fmt.Printf("Converted %s into %s\n", inputFile, outputFile)
}

// resolveDomain returns the first IP address found for the given domain.
func resolveDomain(domain string) (string, error) {
	addresses, err := net.LookupHost(domain)
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", fmt.Errorf("no IP found for domain: %s", domain)
	}
	return addresses[0], nil // use the first IP found
}

func stringToInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}

// randBucketIndex generates a random bucket index between 0 and 255 (inclusive).
func randBucketIndex() int {
	// CometBFT's addressbook doesn't appear to enforce a range for bucket
	// indexes but this Cosmos Hub address book has bucket indexes between 0
	// and 255.
	//
	// Ref: https://dl2.quicksync.io/json/addrbook.cosmos.json
	return rand.Intn(256)
}
