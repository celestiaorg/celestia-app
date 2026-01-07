package main

import (
	"encoding/json"
	"fmt"
)

type targetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func buildMetricsTargets(cfg Config, port int, addressSource string) ([]targetGroup, int, error) {
	if addressSource != "public" && addressSource != "private" {
		return nil, 0, fmt.Errorf("invalid address source %q (use public or private)", addressSource)
	}

	var groups []targetGroup
	var skipped int

	appendTargets := func(nodes []Instance, role string) {
		for _, node := range nodes {
			address, ok := nodeAddress(node, port, addressSource)
			if !ok {
				skipped++
				continue
			}

			groups = append(groups, targetGroup{
				Targets: []string{address},
				Labels: map[string]string{
					"chain_id":   cfg.ChainID,
					"experiment": cfg.Experiment,
					"role":       role,
					"region":     node.Region,
					"provider":   string(node.Provider),
					"node_id":    node.Name,
				},
			})
		}
	}

	appendTargets(cfg.Validators, "validator")
	appendTargets(cfg.Bridges, "bridge")
	appendTargets(cfg.Lights, "light")

	return groups, skipped, nil
}

func marshalTargets(groups []targetGroup, pretty bool) ([]byte, error) {
	if pretty {
		return json.MarshalIndent(groups, "", "  ")
	}
	return json.Marshal(groups)
}

func nodeAddress(node Instance, port int, source string) (string, bool) {
	var ip string
	switch source {
	case "public":
		ip = node.PublicIP
		if ip == "" || ip == "TBD" {
			ip = node.PrivateIP
		}
	case "private":
		ip = node.PrivateIP
		if ip == "" || ip == "TBD" {
			ip = node.PublicIP
		}
	}

	if ip == "" || ip == "TBD" {
		return "", false
	}

	return fmt.Sprintf("%s:%d", ip, port), true
}
