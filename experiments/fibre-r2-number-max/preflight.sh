#!/usr/bin/env bash
# Checks instance availability, vCPU quota, and estimated EC2 cost before any resources are created.
# Usage: ./preflight.sh [zone] [hours]
set -euo pipefail

ZONE="${1:-us-east-2a}"
HOURS="${2:-4}"
REGION="${ZONE%?}"
BUDGET_USD="${BUDGET_USD:-250}"
VALIDATOR_TYPE="${VALIDATOR_TYPE:-c8gn.24xlarge}"
ENCODER_TYPE="${ENCODER_TYPE:-c8gn.48xlarge}"
OBSERVABILITY_TYPE="${OBSERVABILITY_TYPE:-t3.medium}"
VALIDATORS="${VALIDATORS:-3}"

# name|type|count
NODES="validator|$VALIDATOR_TYPE|$VALIDATORS
encoder|$ENCODER_TYPE|1
observability|$OBSERVABILITY_TYPE|1"

fail=0
total_vcpu=0
hourly=0

echo "Preflight for $ZONE, $HOURS hour(s), budget \$$BUDGET_USD"
while IFS='|' read -r name type count; do
  offered=$(aws ec2 describe-instance-type-offerings --region "$REGION" --location-type availability-zone \
    --filters "Name=location,Values=$ZONE" "Name=instance-type,Values=$type" \
    --query 'length(InstanceTypeOfferings)' --output text)
  if [ "$offered" = "0" ]; then
    echo "FAIL: $type is not offered in $ZONE"
    fail=1
    continue
  fi
  # Network performance goes last because it contains spaces.
  read -r vcpu mem_mib arch net < <(aws ec2 describe-instance-types --region "$REGION" --instance-types "$type" \
    --query 'InstanceTypes[0].[VCpuInfo.DefaultVCpus,MemoryInfo.SizeInMiB,ProcessorInfo.SupportedArchitectures[0],NetworkInfo.NetworkPerformance]' \
    --output text)
  price=$(aws pricing get-products --region us-east-1 --service-code AmazonEC2 \
    --filters "Type=TERM_MATCH,Field=instanceType,Value=$type" "Type=TERM_MATCH,Field=regionCode,Value=$REGION" \
    "Type=TERM_MATCH,Field=operatingSystem,Value=Linux" "Type=TERM_MATCH,Field=tenancy,Value=Shared" \
    "Type=TERM_MATCH,Field=preInstalledSw,Value=NA" "Type=TERM_MATCH,Field=capacitystatus,Value=Used" \
    --query 'PriceList[0]' --output text |
    jq -r '.terms.OnDemand[].priceDimensions[].pricePerUnit.USD')
  echo "OK:   $count x $name $type ($arch, $vcpu vCPU, $((mem_mib / 1024)) GiB, $net) at \$$price/h"
  total_vcpu=$((total_vcpu + vcpu * count))
  hourly=$(echo "$hourly + $price * $count" | bc -l)
done <<< "$NODES"

# Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances.
quota=$(aws service-quotas get-service-quota --region "$REGION" --service-code ec2 --quota-code L-1216C47A \
  --query 'Quota.Value' --output text)
if [ "${quota%.*}" -lt "$total_vcpu" ]; then
  echo "FAIL: need $total_vcpu On-Demand vCPUs, quota is $quota"
  fail=1
else
  echo "OK:   need $total_vcpu On-Demand vCPUs, quota is $quota"
fi

estimate=$(echo "$hourly * $HOURS" | bc -l)
printf "EC2 estimate: \$%.2f/h x %s h = \$%.2f\n" "$hourly" "$HOURS" "$estimate"
if [ "$(echo "$estimate >= $BUDGET_USD" | bc -l)" = "1" ]; then
  echo "FAIL: estimate exceeds the \$$BUDGET_USD budget"
  fail=1
fi
exit $fail
