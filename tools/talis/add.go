package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

func addCmd() *cobra.Command {
	var (
		rootDir  string
		count    int
		nodeType string
		provider string
		region   string
	)
	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Adds a new instances to the configuration",
		Aliases: []string{"a"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config %q: %w", rootDir, err)
			}

			if provider == "" {
				provider = "digitalocean"
			}

			switch nodeType {
			case "validator":
				for i := 0; i < count; i++ {
					switch provider {
					case "digitalocean":
						cfg = cfg.WithDigitalOceanValidator(region)
					case "googlecloud":
						cfg = cfg.WithGoogleCloudValidator(region)
					default:
						return fmt.Errorf("unknown provider %q (supported: digitalocean, googlecloud)", provider)
					}
				}
			case "bridge":
				log.Println("bridges are not yet supported")
				return nil
			case "light":
				log.Println("light nodes are not yet supported")
				return nil
			default:
				return fmt.Errorf("unknown node type %q", nodeType)
			}

			return cfg.Save(rootDir)
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to initialize")
	cmd.Flags().IntVarP(&count, "count", "c", 0, "Number of nodes to deploy")
	_ = cmd.MarkFlagRequired("count")
	cmd.Flags().StringVarP(&nodeType, "type", "t", "", "Type of the node (validator, bridge, light)")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVarP(&provider, "provider", "p", "digitalocean", "Provider for the node (digitalocean, googlecloud)")
	cmd.Flags().StringVarP(&region, "region", "r", "random", "the region to deploy the instance in (random if blank)")

	return cmd
}
