package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

type S3Config struct {
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	BucketName      string `json:"bucket_name"`
	Endpoint        string `json:"endpoint"`
}

// downloadS3DataCmd creates a cobra command for downloading a chain's data from S3.
func downloadS3DataCmd() *cobra.Command {
	var (
		rootDir string
		cfgPath string
		outDir  string
		chainID string
	)

	cmd := &cobra.Command{
		Use:   "s3",
		Short: "Download all files from S3 under <bucket>/<chain-id> into a local directory",
		Long: `Loads the network config, instantiates an AWS S3 client using the
credentials in it, then recursively downloads everything under
"<bucket>/<chain-id>/" into the output directory you specify.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(rootDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			client, err := createS3Client(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("failed to create S3 client: %w", err)
			}
			if chainID != "" {
				cfg.ChainID = chainID
			}

			// 4. Compute prefix and download
			prefix := cfg.ChainID + "/"
			if err := downloadS3Directory(cmd.Context(), client, cfg.S3Config.BucketName, prefix, outDir); err != nil {
				return fmt.Errorf("failed to download S3 objects: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&rootDir, "directory", "d", ".", "root directory in which to find your config.json")
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config.json", "name of the config file (under the directory)")
	cmd.Flags().StringVarP(&outDir, "out", "o", "./data", "local directory into which to download the S3 objects")
	cmd.Flags().StringVarP(&chainID, "chain-id", "i", "", "override the chain-id in the config")

	return cmd
}

// downloadS3Directory lists and downloads all objects under the given prefix.
func downloadS3Directory(ctx context.Context, client *s3.Client, bucket, prefix, dest string) error {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		for _, obj := range page.Contents {
			// compute local file path: strip the prefix
			relPath := strings.TrimPrefix(*obj.Key, prefix)
			if relPath == "" {
				// skip the "directory" marker itself
				continue
			}
			localPath := filepath.Join(dest, relPath)
			if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
				return err
			}

			// download each object
			f, err := os.Create(localPath)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			}, func(o *s3.Options) {
				// no special options
			})
			if err != nil {
				return err
			}

			// stream body into file
			downloader := manager.NewDownloader(client)
			_, err = downloader.Download(ctx, f,
				&s3.GetObjectInput{Bucket: aws.String(bucket), Key: obj.Key},
			)
			if err != nil {
				return fmt.Errorf("download %s: %w", *obj.Key, err)
			}

			log.Println("Downloaded", *obj.Key)
		}
	}

	return nil
}

func createS3Client(ctx context.Context, cfg Config) (*s3.Client, error) {
	s3cfg := cfg.S3Config
	var awsCfg aws.Config
	var err error
	if cfg.S3Config.Endpoint != "" {
		customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) { //nolint:staticcheck
			return aws.Endpoint{ //nolint:staticcheck
				URL:           cfg.S3Config.Endpoint,
				SigningRegion: region,
			}, nil
		})

		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(s3cfg.Region),
			config.WithCredentialsProvider(
				aws.NewCredentialsCache(
					credentials.NewStaticCredentialsProvider(
						s3cfg.AccessKeyID,
						s3cfg.SecretAccessKey,
						"",
					),
				),
			),
			config.WithEndpointResolverWithOptions(customResolver), //nolint:staticcheck
		)
	} else {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(s3cfg.Region),
			config.WithCredentialsProvider(
				aws.NewCredentialsCache(
					credentials.NewStaticCredentialsProvider(
						s3cfg.AccessKeyID,
						s3cfg.SecretAccessKey,
						"",
					),
				),
			),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build AWS config: %w", err)
	}

	return s3.NewFromConfig(awsCfg), nil
}
