package main

import "math/rand"

const (
	DODefaultValidatorSlug = "c2-16vcpu-32gb"
	DODefaultImage         = "ubuntu-22-04-x64"
)

var (
	DORegions = []string{
		"nyc1", "nyc3", "tor1", "sfo2", "sfo3", "ams3", "sgp1", "lon1", "fra1", "syd1", "blr1",
	}

	DOLargeRegions = map[string]int{
		"nyc3": 6, "tor1": 6, "sfo2": 2, "sfo3": 6, "ams3": 8, "sgp1": 4, "lon1": 8, "fra1": 6, "syd1": 6,
	}

	DOMediumRegions = map[string]int{
		"nyc3": 2, "tor1": 2, "sfo3": 2, "ams3": 2, "lon1": 2,
	}

	DOSmallRegions = map[string]int{
		"ams3": 1, "tor1": 1, "nyc3": 1, "lon1": 1,
	}
)

func RandomDORegion() string {
	return DORegions[rand.Intn(len(DORegions))]
}
