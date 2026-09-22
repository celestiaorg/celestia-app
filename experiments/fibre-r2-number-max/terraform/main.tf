terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = {
      experiment = var.experiment
    }
  }
}

# Shard bucket. Each validator writes under its own <chain-id>/<validator> prefix.
resource "aws_s3_bucket" "shards" {
  bucket_prefix = "${var.experiment}-"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "shards" {
  bucket                  = aws_s3_bucket.shards.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "shards" {
  bucket = aws_s3_bucket.shards.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Safety net in case cleanup is skipped.
resource "aws_s3_bucket_lifecycle_configuration" "shards" {
  bucket = aws_s3_bucket.shards.id
  rule {
    id     = "expire"
    status = "Enabled"
    filter {}
    expiration {
      days = var.object_expiration_days
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

# Route same-region S3 traffic from the default VPC (used by talis) through a gateway endpoint.
data "aws_vpc" "default" {
  default = true
}

data "aws_route_tables" "default" {
  vpc_id = data.aws_vpc.default.id
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = data.aws_vpc.default.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = data.aws_route_tables.default.ids
}

# Instance role that lets fibre read, write, list, and delete shards in the bucket only.
data "aws_iam_policy_document" "assume_ec2" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "shards" {
  statement {
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.shards.arn]
  }
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.shards.arn}/*"]
  }
}

resource "aws_iam_role" "fibre" {
  name_prefix        = "${var.experiment}-"
  assume_role_policy = data.aws_iam_policy_document.assume_ec2.json
}

resource "aws_iam_role_policy" "shards" {
  role   = aws_iam_role.fibre.id
  policy = data.aws_iam_policy_document.shards.json
}

resource "aws_iam_instance_profile" "fibre" {
  name_prefix = "${var.experiment}-"
  role        = aws_iam_role.fibre.name
}
