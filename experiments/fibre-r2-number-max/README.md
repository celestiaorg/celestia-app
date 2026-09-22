# Fibre object storage with 1280 MiB blobs

Runbook for [PROTOCO-2830](https://linear.app/celestia/issue/PROTOCO-2830). It runs three validators that store Fibre shards in S3, one encoder, and 1280 MiB blobs in `us-east-2`.

Target: at least 4.0 GB/s of successful S3 object bytes per validator (12.0 GB/s total) for 600 seconds.

## Build

Integration branch pins [#7808](https://github.com/celestiaorg/celestia-app/pull/7808) at `38e43d16` and [#7913](https://github.com/celestiaorg/celestia-app/pull/7913) at `99969ce9`. All nodes are Graviton (arm64).

```sh
TALIS_GOARCH=arm64 make build-talis-bins
shasum -a 256 build/* > build/SHA256SUMS
```

## Preflight

Checks AZ offerings, the On-Demand vCPU quota, and the EC2 estimate against a $250 budget.

```sh
./experiments/fibre-r2-number-max/preflight.sh us-east-2a 4
```

## Infrastructure

Terraform creates the shard bucket, an S3 gateway endpoint in the default VPC, and an instance profile scoped to the bucket.

```sh
cd experiments/fibre-r2-number-max/terraform
terraform init && terraform apply
BUCKET=$(terraform output -raw bucket)
PROFILE=$(terraform output -raw instance_profile)
```

## Network

```sh
talis init -c fibre-r2 -e fibre-r2-number-max -p aws --aws-zone us-east-2a --with-observability --observability-region us-east-2
# Set "aws_region": "us-east-2" and "aws_instance_profile": "$PROFILE" in config.json.
talis add -t validator -c 3 -p aws -r us-east-2 --slug c8gn.24xlarge
talis add -t encoder -c 1 -p aws -r us-east-2 --slug c8gn.48xlarge
talis up
talis genesis --square-size 256 --build-dir build
talis deploy --direct-payload-upload --workers 20
talis setup-fibre
talis start-fibre --experimental-max-blob-size-mib 1280 --object-storage-bucket "$BUCKET" --object-storage-region us-east-2
```

Each validator writes under `s3://$BUCKET/fibre-r2/<validator-name>/`.

## Load

Ramp through 0.5, 1, 2, and 4 GB/s per validator for 30–60 seconds each, then warm up and run a fresh 600-second window.

```sh
talis fibre-txsim --on-encoders --instances 1 --blob-size 1342177275 --experimental-max-blob-size-mib 1280 --concurrency <n>
talis fibre-throughput
```

## Cleanup

```sh
talis kill-session --session fibre-txsim
talis kill-session --session fibre
talis down --workers 20
cd experiments/fibre-r2-number-max/terraform && terraform destroy
```

`force_destroy` empties the bucket on destroy. A lifecycle rule also expires objects after two days.
