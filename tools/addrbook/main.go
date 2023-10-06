package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Address struct {
	ID   string `json:"id"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type Entry struct {
	Addr Address `json:"addr"`
	Src  Address `json:"src"`
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
		ip := ipPort[0]
		port := ipPort[1]

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

func stringToInt(s string) int {
	var result int
	fmt.Sscanf(s, "%d", &result)
	return result
}
