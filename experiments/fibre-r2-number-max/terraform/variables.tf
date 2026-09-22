variable "region" {
  description = "AWS region for the bucket and VPC endpoint. Must match talis aws_region."
  type        = string
  default     = "us-east-2"
}

variable "experiment" {
  description = "Name used for resource prefixes and the experiment tag."
  type        = string
  default     = "fibre-r2-number-max"
}

variable "object_expiration_days" {
  description = "Days after which the lifecycle rule deletes shard objects."
  type        = number
  default     = 2
}
