package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
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
	data, err := os.ReadFile("peers.txt")
	if err != nil {
		panic(err)
	}

	lines := strings.Split(string(data), "\n")

	var addrs []Entry
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "@")
		id := parts[0]
		ipPort := strings.Split(parts[1], ":")
		domain := ipPort[0]
		port := ipPort[1]

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
			Addr: addr,
			Src:  addr,
		}

		addrs = append(addrs, entry)
	}

	output := Output{
		Key:   "075f251a11c6b2cef94f3d61", // This is hard-coded, change as needed
		Addrs: addrs,
	}

	jsonData, err := json.MarshalIndent(output, "", "\t")
	if err != nil {
		panic(err)
	}

	// Save the output to addrbook.json
	if err := os.WriteFile("addrbook.json", jsonData, 0644); err != nil {
		panic(err)
	}
	fmt.Println("Conversion completed. Check addrbook.json.")
}

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
