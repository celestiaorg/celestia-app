output "bucket" {
  description = "Pass to talis start-fibre --object-storage-bucket."
  value       = aws_s3_bucket.shards.id
}

output "instance_profile" {
  description = "Set as aws_instance_profile in talis config.json."
  value       = aws_iam_instance_profile.fibre.name
}

output "s3_vpc_endpoint" {
  value = aws_vpc_endpoint.s3.id
}
