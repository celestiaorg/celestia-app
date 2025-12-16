package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v6/metrics/client"
)

func metricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Manage metrics collection",
		Long:  "Commands for managing Prometheus metrics collection from Celestia nodes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		metricsRegisterCmd(),
		metricsDeregisterCmd(),
		metricsListCmd(),
		metricsRegisterAllCmd(),
	)

	return cmd
}

func metricsRegisterCmd() *cobra.Command {
	var (
		metricsServer string
		nodeID        string
		address       string
		labels        []string
	)

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a node with the metrics server",
		Long:  "Register a Celestia node's Prometheus endpoint with the metrics server for scraping.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New(metricsServer)
			if err != nil {
				return fmt.Errorf("failed to connect to metrics server: %w", err)
			}
			defer c.Close()

			labelMap := parseLabels(labels)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := c.Register(ctx, nodeID, address, labelMap); err != nil {
				return fmt.Errorf("failed to register node: %w", err)
			}

			fmt.Printf("registered node %s at %s\n", nodeID, address)
			return nil
		},
	}

	cmd.Flags().StringVarP(&metricsServer, "server", "s", "localhost:9900", "metrics server address")
	cmd.Flags().StringVarP(&nodeID, "node-id", "n", "", "unique node identifier")
	cmd.Flags().StringVarP(&address, "address", "a", "", "node Prometheus endpoint (host:port)")
	cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "additional labels in key=value format")

	_ = cmd.MarkFlagRequired("node-id")
	_ = cmd.MarkFlagRequired("address")

	return cmd
}

func metricsDeregisterCmd() *cobra.Command {
	var (
		metricsServer string
		nodeID        string
	)

	cmd := &cobra.Command{
		Use:   "deregister",
		Short: "Deregister a node from the metrics server",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New(metricsServer)
			if err != nil {
				return fmt.Errorf("failed to connect to metrics server: %w", err)
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := c.Deregister(ctx, nodeID); err != nil {
				return fmt.Errorf("failed to deregister node: %w", err)
			}

			fmt.Printf("deregistered node %s\n", nodeID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&metricsServer, "server", "s", "localhost:9900", "metrics server address")
	cmd.Flags().StringVarP(&nodeID, "node-id", "n", "", "node identifier to deregister")

	_ = cmd.MarkFlagRequired("node-id")

	return cmd
}

func metricsListCmd() *cobra.Command {
	var metricsServer string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New(metricsServer)
			if err != nil {
				return fmt.Errorf("failed to connect to metrics server: %w", err)
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			targets, err := c.ListTargets(ctx)
			if err != nil {
				return fmt.Errorf("failed to list targets: %w", err)
			}

			if len(targets) == 0 {
				fmt.Println("no nodes registered")
				return nil
			}

			fmt.Printf("%-20s %-25s %-30s %s\n", "NODE ID", "ADDRESS", "REGISTERED", "LABELS")
			fmt.Println(strings.Repeat("-", 100))
			for _, t := range targets {
				labels := formatLabels(t.Labels)
				fmt.Printf("%-20s %-25s %-30s %s\n",
					t.NodeID,
					t.Address,
					t.RegisteredAt.Format(time.RFC3339),
					labels,
				)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&metricsServer, "server", "s", "localhost:9900", "metrics server address")

	return cmd
}

func metricsRegisterAllCmd() *cobra.Command {
	var (
		metricsServer string
		rootDir       string
	)

	cmd := &cobra.Command{
		Use:   "register-all",
		Short: "Register all nodes from a Talis deployment",
		Long:  "Register all nodes from a Talis deployment with the metrics server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load the Talis config to get node information
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			c, err := client.New(metricsServer)
			if err != nil {
				return fmt.Errorf("failed to connect to metrics server: %w", err)
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			registered := 0

			// Register all validators
			for _, node := range cfg.Validators {
				if node.PublicIP == "" || node.PublicIP == "TBD" {
					continue
				}
				if err := registerNode(ctx, c, node, cfg.ChainID, "validator"); err != nil {
					fmt.Printf("warning: failed to register %s: %v\n", node.Name, err)
					continue
				}
				fmt.Printf("registered %s at %s:26660\n", node.Name, node.PublicIP)
				registered++
			}

			// Register all bridges
			for _, node := range cfg.Bridges {
				if node.PublicIP == "" || node.PublicIP == "TBD" {
					continue
				}
				if err := registerNode(ctx, c, node, cfg.ChainID, "bridge"); err != nil {
					fmt.Printf("warning: failed to register %s: %v\n", node.Name, err)
					continue
				}
				fmt.Printf("registered %s at %s:26660\n", node.Name, node.PublicIP)
				registered++
			}

			// Register all light nodes
			for _, node := range cfg.Lights {
				if node.PublicIP == "" || node.PublicIP == "TBD" {
					continue
				}
				if err := registerNode(ctx, c, node, cfg.ChainID, "light"); err != nil {
					fmt.Printf("warning: failed to register %s: %v\n", node.Name, err)
					continue
				}
				fmt.Printf("registered %s at %s:26660\n", node.Name, node.PublicIP)
				registered++
			}

			fmt.Printf("\nregistered %d nodes\n", registered)
			return nil
		},
	}

	cmd.Flags().StringVarP(&metricsServer, "server", "s", "localhost:9900", "metrics server address")
	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "Talis root directory")

	return cmd
}

func registerNode(ctx context.Context, c *client.Client, node Instance, chainID, role string) error {
	labels := map[string]string{
		"chain_id": chainID,
		"role":     role,
		"region":   node.Region,
		"provider": string(node.Provider),
	}

	// Prometheus endpoint is on port 26660
	address := fmt.Sprintf("%s:26660", node.PublicIP)

	return c.Register(ctx, node.Name, address, labels)
}

func parseLabels(labels []string) map[string]string {
	result := make(map[string]string)
	for _, l := range labels {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ", ")
}
