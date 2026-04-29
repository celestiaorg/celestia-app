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
		slug     string
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
				start := len(cfg.Validators)
				for range count {
					switch provider {
					case "digitalocean":
						cfg = cfg.WithDigitalOceanValidator(region)
					case "googlecloud":
						cfg = cfg.WithGoogleCloudValidator(region)
					case "aws":
						cfg = cfg.WithAWSValidator(region)
					default:
						return fmt.Errorf("unknown provider %q (supported: digitalocean, googlecloud, aws)", provider)
					}
				}
				applySlug(cfg.Validators, start, slug)
			case "encoder":
				start := len(cfg.Encoders)
				for range count {
					switch provider {
					case "digitalocean":
						cfg = cfg.WithDigitalOceanEncoder(region)
					case "googlecloud":
						cfg = cfg.WithGoogleCloudEncoder(region)
					case "aws":
						cfg = cfg.WithAWSEncoder(region)
					default:
						return fmt.Errorf("unknown provider %q (supported: digitalocean, googlecloud, aws)", provider)
					}
				}
				applySlug(cfg.Encoders, start, slug)
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
	cmd.Flags().StringVarP(&nodeType, "type", "t", "", "Type of the node (validator, encoder, bridge, light)")
	_ = cmd.MarkFlagRequired("type")
	cmd.Flags().StringVarP(&provider, "provider", "p", "digitalocean", "Provider for the node (digitalocean, googlecloud, aws)")
	cmd.Flags().StringVarP(&region, "region", "r", "random", "the region to deploy the instance in (random if blank)")
	cmd.Flags().StringVar(&slug, "slug", "", "provider-specific instance type override (e.g. c6in.4xlarge). Empty = provider default for the node type.")

	return cmd
}

// applySlug overrides the Slug field on the just-added instances in the
// slice. It only touches entries at index [start, len(instances)) so a
// second `add` with a different `--slug` does not re-stamp earlier ones.
func applySlug(instances []Instance, start int, slug string) {
	if slug == "" {
		return
	}
	for i := start; i < len(instances); i++ {
		instances[i].Slug = slug
	}
}
