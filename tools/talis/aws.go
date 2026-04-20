package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	// i4i.4xlarge: 16 vCPU / 128 GiB / up to 25 Gbps network, and a
	// 3.75 TB local NVMe instance-store that delivers ~3 GB/s write.
	// Fibre's per-shard path is dominated by pebble store_put; on
	// c6in+gp3 the 125 MB/s EBS ceiling caps upload_shard long before
	// the network saturates. Local NVMe moves the disk ceiling back
	// into line with the network.
	//
	// The NVMe is ephemeral — fine here because `talis down` always
	// terminates the instance and experiments always re-run genesis.
	AWSDefaultValidatorInstanceType     = "i4i.4xlarge"
	AWSDefaultEncoderInstanceType       = "i4i.4xlarge"
	AWSDefaultObservabilityInstanceType = "t3.medium"
	// Root EBS only holds the OS + /root/payload.tar.gz; fibre /
	// celestia state lives on the local NVMe mounted at /mnt/data.
	AWSDefaultRootVolumeGB = int32(50)

	// AWSSecurityGroupName is the name of the security group used by every
	// talis instance. It is created per-region on demand and permits all
	// inbound traffic — same posture as the GCP firewall rule.
	AWSSecurityGroupName = "talis-allow-all"
	// AWSPlacementGroupName is the name of the cluster placement group used
	// by every talis instance in a region. Cluster strategy gives the lowest
	// inter-instance latency within an AZ — critical for fibre/p2p.
	AWSPlacementGroupName = "talis-cluster"

	// AWSCanonicalOwnerID is Canonical's AWS account ID. It owns the
	// official Ubuntu AMIs we filter against.
	AWSCanonicalOwnerID = "099720109477"
	// AWSUbuntuImageNamePattern matches Ubuntu 24.04 LTS amd64 EBS SSD
	// images (matches talis' default OS image for DO / GCP).
	AWSUbuntuImageNamePattern = "ubuntu/images/hvm-ssd*/ubuntu-noble-24.04-amd64-server-*"

	// AWSDefaultZone is the AZ used for launches when Config.AWSZone is
	// unset. Single-AZ launches keep all cross-instance traffic intra-AZ
	// (free) and enable a cluster placement group for minimum latency.
	AWSDefaultZone = "us-east-1a"
)

// AWSRegions is the pool used when "random" is selected for an AWS
// instance. We ship a single region by default: cross-region traffic on
// AWS is billed at $0.09/GB (~9× DO), so running networking-heavy
// experiments across regions is wildly expensive. Operators who need
// multi-region can set an explicit Region on each Instance.
var AWSRegions = []string{"us-east-1"}

// amiCache memoises the resolved Ubuntu AMI per region — AMIs are
// region-scoped and resolving them costs an API round-trip.
var amiCache sync.Map // map[region]string

type AWSClient struct {
	ClientInfo
	defaultRegion string
}

func NewAWSClient(cfg Config) (*AWSClient, error) {
	if cfg.AWSRegion == "" {
		return nil, errors.New("AWS region is required")
	}
	sshKey, err := os.ReadFile(cfg.SSHPubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH public key at %s: %w", cfg.SSHPubKeyPath, err)
	}
	return &AWSClient{
		ClientInfo: ClientInfo{
			sshKey: sshKey,
			cfg:    cfg,
		},
		defaultRegion: cfg.AWSRegion,
	}, nil
}

func (c *AWSClient) Up(ctx context.Context, workers int) error {
	zone := c.cfg.AWSZone
	if zone == "" {
		zone = AWSDefaultZone
	}

	insts := make([]Instance, 0)
	allInstances := append(append(c.cfg.Validators, c.cfg.Observability...), c.cfg.Encoders...)
	for _, v := range allInstances {
		if v.Provider != AWS {
			continue
		}
		if v.Region == "" || v.Region == RandomRegion {
			v.Region = RandomAWSRegion()
		}
		if v.Zone == "" {
			v.Zone = zone
		}
		insts = append(insts, v)
	}

	if len(insts) == 0 {
		return fmt.Errorf("no instances to create")
	}

	insts, err := CreateAWSInstances(ctx, insts, string(c.sshKey), c.cfg.SSHKeyName, workers)
	if err != nil {
		return fmt.Errorf("failed to create instances: %w", err)
	}

	for _, inst := range insts {
		cfg, err := c.cfg.UpdateInstance(inst.Name, inst.PublicIP, inst.PrivateIP)
		if err != nil {
			return fmt.Errorf("failed to update config with instance %s: %w", inst.Name, err)
		}
		c.cfg = cfg
	}
	return nil
}

func (c *AWSClient) Down(ctx context.Context, workers int) error {
	insts := make([]Instance, 0)
	allInstances := append(append(c.cfg.Validators, c.cfg.Observability...), c.cfg.Encoders...)
	for _, v := range allInstances {
		if v.Provider != AWS {
			continue
		}
		if v.Region == "" || v.Region == RandomRegion {
			v.Region = RandomAWSRegion()
		}
		insts = append(insts, v)
	}
	if len(insts) == 0 {
		return fmt.Errorf("no instances to destroy")
	}
	_, err := DestroyAWSInstances(ctx, insts, workers)
	return err
}

func (c *AWSClient) List(ctx context.Context) error {
	cnt := 0
	for _, region := range AWSRegions {
		client, err := newEC2Client(ctx, region)
		if err != nil {
			return fmt.Errorf("failed to create EC2 client in %s: %w", region, err)
		}
		insts, err := describeTalisInstances(ctx, client)
		if err != nil {
			return fmt.Errorf("describe instances in %s: %w", region, err)
		}
		for _, inst := range insts {
			if cnt == 0 {
				fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "Name", "Status", "Region", "Public IP", "Created")
				fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "----", "------", "------", "---------", "-------")
			}
			state := ""
			if inst.State != nil {
				state = string(inst.State.Name)
			}
			publicIP := ""
			if inst.PublicIpAddress != nil {
				publicIP = *inst.PublicIpAddress
			}
			created := ""
			if inst.LaunchTime != nil {
				created = inst.LaunchTime.Format(time.RFC3339)
			}
			fmt.Printf("%-30s %-10s %-15s %-15s %s\n",
				instanceNameFromTags(inst.Tags), state, region, publicIP, created)
			cnt++
		}
	}
	fmt.Println("Total number of talis instances:", cnt)
	return nil
}

func (c *AWSClient) GetConfig() Config {
	return c.cfg
}

func NewAWSValidator(region string) Instance {
	if region == "" || region == RandomRegion {
		region = RandomAWSRegion()
	}
	i := NewBaseInstance(Validator)
	i.Provider = AWS
	i.Slug = AWSDefaultValidatorInstanceType
	i.Region = region
	return i
}

func NewAWSEncoder(region string) Instance {
	if region == "" || region == RandomRegion {
		region = RandomAWSRegion()
	}
	i := NewBaseInstance(Encoder)
	i.Provider = AWS
	i.Slug = AWSDefaultEncoderInstanceType
	i.Region = region
	return i
}

func NewAWSObservability(region string) Instance {
	if region == "" || region == RandomRegion {
		region = RandomAWSRegion()
	}
	i := NewBaseInstance(Observability)
	i.Provider = AWS
	i.Slug = AWSDefaultObservabilityInstanceType
	i.Region = region
	return i
}

func RandomAWSRegion() string {
	return AWSRegions[rand.Intn(len(AWSRegions))]
}

// awsRegionFromEnv returns the region stamped into Config when
// `--provider aws` is selected. Falls back to us-east-1 to match AWS
// SDK's historical implicit default.
func awsRegionFromEnv() string {
	if r := os.Getenv(EnvVarAWSRegion); r != "" {
		return r
	}
	return "us-east-1"
}

// resolveAWSZone returns the given zone or AWSDefaultZone.
func resolveAWSZone(zone string) string {
	if zone != "" {
		return zone
	}
	return AWSDefaultZone
}

// newEC2Client constructs a regional EC2 client using the SDK default
// credential chain (env vars, shared credentials file, IAM role, ...).
func newEC2Client(ctx context.Context, region string) (*ec2.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for region %s: %w", region, err)
	}
	return ec2.NewFromConfig(awsCfg), nil
}

// CreateAWSInstances launches EC2 instances in parallel, each pinned to
// its Instance.Zone + the cluster placement group (where supported),
// waits for public + private IPs, and returns the filled-in slice.
func CreateAWSInstances(ctx context.Context, insts []Instance, sshKey, keyName string, workers int) ([]Instance, error) {
	type result struct {
		inst         Instance
		err          error
		timeRequired time.Duration
	}

	insts, existing, err := filterExistingAWSInstances(ctx, insts)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		log.Println("Existing instances found, so they are not being created.")
		for _, v := range existing {
			log.Println("Skipping", v.Name, v.PublicIP, v.Tags)
		}
	}

	total := len(insts)
	results := make(chan result, total)
	workerChan := make(chan struct{}, workers)
	var wg sync.WaitGroup
	wg.Add(total)

	for _, v := range insts {
		go func(inst Instance) {
			workerChan <- struct{}{}
			defer func() {
				<-workerChan
				wg.Done()
			}()

			ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
			defer cancel()

			start := time.Now()
			log.Println("Creating instance", inst.Name, "in region", inst.Region, start.Format(time.RFC3339))

			pubIP, privIP, err := createAWSInstance(ctx, inst, sshKey, keyName)
			if err != nil {
				results <- result{inst: inst, err: fmt.Errorf("create %s: %w", inst.Name, err)}
				return
			}
			inst.PublicIP = pubIP
			inst.PrivateIP = privIP
			results <- result{inst: inst, err: nil, timeRequired: time.Since(start)}
		}(v)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var created []Instance
	for res := range results {
		if res.err != nil {
			fmt.Printf("❌ %s failed after %v %v\n", res.inst.Name, res.timeRequired, res.err)
		} else {
			created = append(created, res.inst)
			fmt.Printf("✅ %s is up (public=%s) in %v\n", res.inst.Name, res.inst.PublicIP, res.timeRequired)
		}
		fmt.Printf("---- Progress: %d/%d\n", len(created), total)
	}
	return created, nil
}

// createAWSInstance runs the full per-instance provisioning: resolve
// AMI, ensure key pair + security group + placement group, resolve
// default subnet in the target AZ, RunInstances, wait for IPs.
func createAWSInstance(ctx context.Context, inst Instance, sshKey, keyName string) (string, string, error) {
	client, err := newEC2Client(ctx, inst.Region)
	if err != nil {
		return "", "", err
	}

	amiID, err := resolveUbuntuAMI(ctx, client, inst.Region)
	if err != nil {
		return "", "", fmt.Errorf("resolve AMI: %w", err)
	}
	if err := ensureAWSKeyPair(ctx, client, keyName, sshKey); err != nil {
		return "", "", fmt.Errorf("ensure key pair: %w", err)
	}
	sgID, err := ensureAWSSecurityGroup(ctx, client)
	if err != nil {
		return "", "", fmt.Errorf("ensure security group: %w", err)
	}

	useCPG := supportsClusterPlacement(inst.Slug)
	if useCPG {
		if err := ensureAWSPlacementGroup(ctx, client); err != nil {
			return "", "", fmt.Errorf("ensure placement group: %w", err)
		}
	}

	zone := inst.Zone
	if zone == "" {
		zone = AWSDefaultZone
	}
	subnetID, err := defaultSubnetInAZ(ctx, client, zone)
	if err != nil {
		return "", "", fmt.Errorf("resolve subnet in %s: %w", zone, err)
	}

	tags := awsTagsFromInstance(inst)
	userData := base64.StdEncoding.EncodeToString([]byte(awsRootSSHUserData(sshKey, inst.Name)))

	placement := &ec2types.Placement{AvailabilityZone: aws.String(zone)}
	if useCPG {
		placement.GroupName = aws.String(AWSPlacementGroupName)
	}

	runOut, err := client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      aws.String(amiID),
		InstanceType: ec2types.InstanceType(inst.Slug),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		KeyName:      aws.String(keyName),
		UserData:     aws.String(userData),
		// Use a single NIC so we can force public-IP assignment regardless
		// of the subnet's MapPublicIpOnLaunch setting. SubnetId and
		// SecurityGroupIds must live on the interface — the API rejects
		// both top-level and interface-level settings together.
		NetworkInterfaces: []ec2types.InstanceNetworkInterfaceSpecification{{
			DeviceIndex:              aws.Int32(0),
			SubnetId:                 aws.String(subnetID),
			Groups:                   []string{sgID},
			AssociatePublicIpAddress: aws.Bool(true),
			DeleteOnTermination:      aws.Bool(true),
		}},
		Placement: placement,
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{{
			DeviceName: aws.String("/dev/sda1"),
			Ebs: &ec2types.EbsBlockDevice{
				VolumeSize:          aws.Int32(AWSDefaultRootVolumeGB),
				VolumeType:          ec2types.VolumeTypeGp3,
				DeleteOnTermination: aws.Bool(true),
			},
		}},
		MetadataOptions: &ec2types.InstanceMetadataOptionsRequest{
			HttpTokens:   ec2types.HttpTokensStateRequired,
			HttpEndpoint: ec2types.InstanceMetadataEndpointStateEnabled,
		},
		TagSpecifications: []ec2types.TagSpecification{
			{ResourceType: ec2types.ResourceTypeInstance, Tags: tags},
			{ResourceType: ec2types.ResourceTypeVolume, Tags: tags},
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("run instance: %w", err)
	}
	if len(runOut.Instances) == 0 || runOut.Instances[0].InstanceId == nil {
		return "", "", fmt.Errorf("RunInstances returned no instances")
	}

	return waitForAWSNetworkIP(ctx, client, *runOut.Instances[0].InstanceId)
}

// supportsClusterPlacement reports whether the given EC2 instance type
// can join a cluster placement group. Cluster placement groups require
// compute/network-optimised families; burstable (t*) is explicitly
// rejected by the API. Observability nodes default to t3.medium, which
// falls back to AZ-only placement.
func supportsClusterPlacement(slug string) bool {
	return slug != "" && !strings.HasPrefix(slug, "t")
}

func waitForAWSNetworkIP(ctx context.Context, client *ec2.Client, instanceID string) (string, string, error) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-ticker.C:
			out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
				InstanceIds: []string{instanceID},
			})
			if err != nil {
				return "", "", err
			}
			inst, ok := firstInstance(out)
			if !ok {
				continue
			}
			var pubIP, privIP string
			if inst.PublicIpAddress != nil {
				pubIP = *inst.PublicIpAddress
			}
			if inst.PrivateIpAddress != nil {
				privIP = *inst.PrivateIpAddress
			}
			if pubIP != "" && privIP != "" {
				return pubIP, privIP, nil
			}
		}
	}
}

func firstInstance(out *ec2.DescribeInstancesOutput) (ec2types.Instance, bool) {
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			return i, true
		}
	}
	return ec2types.Instance{}, false
}

// filterExistingAWSInstances removes instances whose experiment tag
// already exists in any region covered by the request. Groups by region
// so each region is queried once.
func filterExistingAWSInstances(ctx context.Context, insts []Instance) ([]Instance, []Instance, error) {
	regions := make(map[string]struct{})
	for _, inst := range insts {
		regions[inst.Region] = struct{}{}
	}

	existingTags := make(map[string]bool)
	for region := range regions {
		client, err := newEC2Client(ctx, region)
		if err != nil {
			return nil, nil, err
		}
		tags, err := collectTalisTagKeys(ctx, client)
		if err != nil {
			return nil, nil, fmt.Errorf("list existing tags in %s: %w", region, err)
		}
		for tag := range tags {
			existingTags[tag] = true
		}
	}

	var newInsts, existing []Instance
	for _, inst := range insts {
		experimentTag := GetExperimentTag(inst.Tags)
		if experimentTag == "" || !existingTags[experimentTag] {
			newInsts = append(newInsts, inst)
		} else {
			existing = append(existing, inst)
		}
	}
	return newInsts, existing, nil
}

func collectTalisTagKeys(ctx context.Context, client *ec2.Client) (map[string]bool, error) {
	out := make(map[string]bool)
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag-key"), Values: []string{"talis"}},
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				for _, t := range i.Tags {
					if t.Key != nil {
						out[*t.Key] = true
					}
				}
			}
		}
	}
	return out, nil
}

func DestroyAWSInstances(ctx context.Context, insts []Instance, workers int) ([]Instance, error) {
	return destroyAWSInstancesInternal(ctx, insts, workers)
}

func destroyAWSInstancesInternal(ctx context.Context, insts []Instance, workers int) ([]Instance, error) {
	type result struct {
		inst         Instance
		err          error
		timeRequired time.Duration
	}

	results := make(chan result, len(insts))
	workerChan := make(chan struct{}, workers)
	var wg sync.WaitGroup
	wg.Add(len(insts))

	for _, inst := range insts {
		go func(inst Instance) {
			workerChan <- struct{}{}
			defer func() {
				<-workerChan
				wg.Done()
			}()
			start := time.Now()
			fmt.Println("⏳ Deleting instance", inst.Name, inst.PublicIP)

			delCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			region := inst.Region
			if region == "" {
				found, err := findAWSInstanceRegion(delCtx, inst.Name)
				if err != nil {
					results <- result{inst: inst, err: fmt.Errorf("find region for %s: %w", inst.Name, err)}
					return
				}
				region = found
			}

			client, err := newEC2Client(delCtx, region)
			if err != nil {
				results <- result{inst: inst, err: fmt.Errorf("ec2 client %s: %w", region, err)}
				return
			}

			instanceID, err := findAWSInstanceID(delCtx, client, inst)
			if err != nil {
				results <- result{inst: inst, err: fmt.Errorf("find instance %s: %w", inst.Name, err)}
				return
			}

			if _, err := client.TerminateInstances(delCtx, &ec2.TerminateInstancesInput{
				InstanceIds: []string{instanceID},
			}); err != nil {
				results <- result{inst: inst, err: fmt.Errorf("terminate %s: %w", inst.Name, err)}
				return
			}
			results <- result{inst: inst, err: nil, timeRequired: time.Since(start)}
		}(inst)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var removed []Instance
	var failed []result
	for res := range results {
		if res.err != nil {
			fmt.Printf("❌ %s failed to delete after %v: %v\n", res.inst.Name, res.timeRequired, res.err)
			failed = append(failed, res)
		} else {
			removed = append(removed, res.inst)
			fmt.Printf("✅ %s terminated (took %v)\n", res.inst.Name, res.timeRequired)
		}
		fmt.Printf("---- Progress: %d/%d\n", len(removed)+len(failed), len(insts))
	}
	return removed, nil
}

// findAWSInstanceID resolves an Instance (by its experiment tag if
// present, otherwise by Name) to an EC2 instance ID. Filters out
// already-terminated instances so repeated calls don't return ghosts.
func findAWSInstanceID(ctx context.Context, client *ec2.Client, inst Instance) (string, error) {
	filters := []ec2types.Filter{
		{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
	}
	if experimentTag := GetExperimentTag(inst.Tags); experimentTag != "" {
		filters = append(filters, ec2types.Filter{Name: aws.String("tag-key"), Values: []string{experimentTag}})
	} else {
		filters = append(filters, ec2types.Filter{Name: aws.String("tag:Name"), Values: []string{inst.Name}})
	}

	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{Filters: filters})
	var ids []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				if i.InstanceId != nil {
					ids = append(ids, *i.InstanceId)
				}
			}
		}
	}

	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no instances found for %s", inst.Name)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("multiple instances match %s: %v", inst.Name, ids)
	}
}

func findAWSInstanceRegion(ctx context.Context, name string) (string, error) {
	for _, region := range AWSRegions {
		client, err := newEC2Client(ctx, region)
		if err != nil {
			continue
		}
		paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("tag:Name"), Values: []string{name}},
				{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
			},
		})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				break
			}
			for _, r := range page.Reservations {
				if len(r.Instances) > 0 {
					return region, nil
				}
			}
		}
	}
	return "", fmt.Errorf("instance %s not found in any known AWS region", name)
}

// destroyAllTalisAWSInstances terminates every EC2 instance tagged
// "talis" across every known region. Called via `down --all`.
func destroyAllTalisAWSInstances(ctx context.Context, workers int) ([]Instance, error) {
	var talisInstances []Instance
	for _, region := range AWSRegions {
		client, err := newEC2Client(ctx, region)
		if err != nil {
			log.Printf("⚠️  failed to build EC2 client for %s: %v", region, err)
			continue
		}
		insts, err := describeTalisInstances(ctx, client)
		if err != nil {
			log.Printf("⚠️  failed to describe instances in %s: %v", region, err)
			continue
		}
		for _, i := range insts {
			publicIP := ""
			if i.PublicIpAddress != nil {
				publicIP = *i.PublicIpAddress
			}
			talisInstances = append(talisInstances, Instance{
				Name:     instanceNameFromTags(i.Tags),
				PublicIP: publicIP,
				Region:   region,
			})
		}
	}

	if len(talisInstances) == 0 {
		log.Println("No talis AWS instances found to destroy")
		return nil, nil
	}
	return destroyAWSInstancesInternal(ctx, talisInstances, workers)
}

func describeTalisInstances(ctx context.Context, client *ec2.Client) ([]ec2types.Instance, error) {
	var out []ec2types.Instance
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag-key"), Values: []string{"talis"}},
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Reservations {
			out = append(out, r.Instances...)
		}
	}
	return out, nil
}

func checkForRunningAWSExperiments(ctx context.Context, awsRegionConfigured bool, experimentID, chainID string) (bool, error) {
	if !awsRegionConfigured {
		return false, nil
	}
	for _, region := range AWSRegions {
		client, err := newEC2Client(ctx, region)
		if err != nil {
			return false, fmt.Errorf("failed to create EC2 client in %s: %w", region, err)
		}
		insts, err := describeTalisInstances(ctx, client)
		if err != nil {
			return false, fmt.Errorf("describe instances in %s: %w", region, err)
		}
		for _, i := range insts {
			for _, t := range i.Tags {
				if t.Key == nil {
					continue
				}
				if hasAWSExperimentTag(*t.Key, experimentID, chainID) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func hasAWSExperimentTag(tag, experimentID, chainID string) bool {
	if !strings.HasPrefix(tag, "validator-") &&
		!strings.HasPrefix(tag, "bridge-") &&
		!strings.HasPrefix(tag, "light-") &&
		!strings.HasPrefix(tag, "encoder-") {
		return false
	}
	return strings.Contains(tag, experimentID) && strings.Contains(tag, chainID)
}

// resolveUbuntuAMI finds the most recent Ubuntu 24.04 AMI in the region.
// Results are cached in-process since AMI IDs rarely change and the
// lookup costs an API round-trip.
func resolveUbuntuAMI(ctx context.Context, client *ec2.Client, region string) (string, error) {
	if cached, ok := amiCache.Load(region); ok {
		return cached.(string), nil
	}

	out, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{AWSCanonicalOwnerID},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{AWSUbuntuImageNamePattern}},
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("architecture"), Values: []string{"x86_64"}},
			{Name: aws.String("virtualization-type"), Values: []string{"hvm"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describe images: %w", err)
	}
	if len(out.Images) == 0 {
		return "", fmt.Errorf("no Ubuntu AMIs found in %s", region)
	}

	sort.Slice(out.Images, func(i, j int) bool {
		a, b := "", ""
		if out.Images[i].CreationDate != nil {
			a = *out.Images[i].CreationDate
		}
		if out.Images[j].CreationDate != nil {
			b = *out.Images[j].CreationDate
		}
		return a > b
	})

	amiID := ""
	if out.Images[0].ImageId != nil {
		amiID = *out.Images[0].ImageId
	}
	if amiID == "" {
		return "", fmt.Errorf("selected AMI has no ID in %s", region)
	}
	amiCache.Store(region, amiID)
	return amiID, nil
}

// ensureAWSKeyPair imports the SSH public key under keyName if it's not
// already registered in the region. EC2 key pairs are region-scoped, so
// this runs once per region.
func ensureAWSKeyPair(ctx context.Context, client *ec2.Client, keyName, publicKey string) error {
	if keyName == "" {
		return errors.New("SSH key name is required for AWS — set via --ssh-key-name or TALIS_SSH_KEY_NAME")
	}
	if _, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keyName},
	}); err == nil {
		return nil
	}
	// Any error is treated as "not found"; let ImportKeyPair surface the
	// real problem if something else is wrong.
	if _, err := client.ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
		KeyName:           aws.String(keyName),
		PublicKeyMaterial: []byte(strings.TrimSpace(publicKey)),
	}); err != nil {
		return fmt.Errorf("import key pair: %w", err)
	}
	return nil
}

// ensureAWSSecurityGroup creates (or looks up) a security group in the
// region's default VPC that allows all inbound traffic from 0.0.0.0/0 —
// same posture as the GCP firewall rule.
func ensureAWSSecurityGroup(ctx context.Context, client *ec2.Client) (string, error) {
	vpcID, err := defaultVPCID(ctx, client)
	if err != nil {
		return "", err
	}

	desc, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("group-name"), Values: []string{AWSSecurityGroupName}},
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	})
	if err == nil && len(desc.SecurityGroups) > 0 && desc.SecurityGroups[0].GroupId != nil {
		return *desc.SecurityGroups[0].GroupId, nil
	}

	create, err := client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(AWSSecurityGroupName),
		Description: aws.String("Talis: allow all inbound traffic on all ports"),
		VpcId:       aws.String(vpcID),
	})
	if err != nil {
		// Another goroutine may have raced us; try to look it up again.
		if desc2, err2 := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("group-name"), Values: []string{AWSSecurityGroupName}},
				{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			},
		}); err2 == nil && len(desc2.SecurityGroups) > 0 && desc2.SecurityGroups[0].GroupId != nil {
			return *desc2.SecurityGroups[0].GroupId, nil
		}
		return "", fmt.Errorf("create security group: %w", err)
	}
	if create.GroupId == nil {
		return "", fmt.Errorf("CreateSecurityGroup returned empty group id")
	}
	groupID := *create.GroupId

	if _, err := client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId: aws.String(groupID),
		IpPermissions: []ec2types.IpPermission{{
			IpProtocol: aws.String("-1"), // all protocols
			IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("0.0.0.0/0")}},
		}},
	}); err != nil && !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
		return "", fmt.Errorf("authorize ingress: %w", err)
	}
	return groupID, nil
}

// ensureAWSPlacementGroup creates a cluster placement group in the
// region if one doesn't already exist. Idempotent and race-safe.
func ensureAWSPlacementGroup(ctx context.Context, client *ec2.Client) error {
	out, err := client.DescribePlacementGroups(ctx, &ec2.DescribePlacementGroupsInput{
		GroupNames: []string{AWSPlacementGroupName},
	})
	if err == nil && len(out.PlacementGroups) > 0 {
		return nil
	}
	if _, err := client.CreatePlacementGroup(ctx, &ec2.CreatePlacementGroupInput{
		GroupName: aws.String(AWSPlacementGroupName),
		Strategy:  ec2types.PlacementStrategyCluster,
	}); err != nil && !strings.Contains(err.Error(), "InvalidPlacementGroup.Duplicate") {
		return fmt.Errorf("create placement group: %w", err)
	}
	return nil
}

// defaultSubnetInAZ returns the SubnetId of the default VPC's default
// subnet in the given AZ. Relies on default-VPC semantics (every account
// has one unless explicitly deleted) rather than managing subnets.
func defaultSubnetInAZ(ctx context.Context, client *ec2.Client, az string) (string, error) {
	out, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("default-for-az"), Values: []string{"true"}},
			{Name: aws.String("availability-zone"), Values: []string{az}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describe subnets: %w", err)
	}
	if len(out.Subnets) == 0 || out.Subnets[0].SubnetId == nil {
		return "", fmt.Errorf("no default subnet in %s — the account may be missing a default VPC/subnet", az)
	}
	return *out.Subnets[0].SubnetId, nil
}

func defaultVPCID(ctx context.Context, client *ec2.Client) (string, error) {
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("is-default"), Values: []string{"true"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describe default VPC: %w", err)
	}
	if len(out.Vpcs) == 0 || out.Vpcs[0].VpcId == nil {
		return "", fmt.Errorf("no default VPC found — create one or extend talis to accept an explicit VPC")
	}
	return *out.Vpcs[0].VpcId, nil
}

func awsTagsFromInstance(inst Instance) []ec2types.Tag {
	tags := make([]ec2types.Tag, 0, len(inst.Tags)+1)
	tags = append(tags, ec2types.Tag{Key: aws.String("Name"), Value: aws.String(inst.Name)})
	for _, t := range inst.Tags {
		tags = append(tags, ec2types.Tag{Key: aws.String(t), Value: aws.String("true")})
	}
	return tags
}

func instanceNameFromTags(tags []ec2types.Tag) string {
	for _, t := range tags {
		if t.Key != nil && *t.Key == "Name" && t.Value != nil {
			return *t.Value
		}
	}
	return ""
}

// awsRootSSHUserData returns cloud-init user-data that at boot time:
//  1. Sets the instance hostname to the talis name. validator_init.sh
//     parses `hostname` to pick per-validator keys; AWS's default
//     `ip-172-…` format breaks that parser.
//  2. Installs the operator's SSH public key into
//     /root/.ssh/authorized_keys so deployment.go can keep using `root@`.
//  3. If a local NVMe instance-store device is present (i-family types
//     like i4i/c6id/im4gn), formats it ext4, mounts it at /mnt/data
//     with `nofail` so reboots can never brick the instance, and
//     creates a `/root/.celestia-fibre` symlink so the fibre server's
//     relative `--home` lands on the fast disk with no code changes on
//     the fibre side. `validator_init.sh` and the generated
//     `encoder_init.sh` detect `/mnt/data` themselves and point
//     `celestia-appd` state there too.
func awsRootSSHUserData(sshKey, instanceName string) string {
	key := strings.TrimSpace(sshKey)
	return fmt.Sprintf(`#cloud-config
disable_root: false
preserve_hostname: false
hostname: %s
fqdn: %s
write_files:
  - path: /usr/local/sbin/talis-setup-nvme.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      # Format and mount the first instance-store NVMe (if any) at
      # /mnt/data. Ephemeral disk — contents are lost on stop/terminate,
      # which is exactly right for talis experiments.
      set -eu
      DEV=""
      for candidate in /dev/nvme1n1 /dev/nvme2n1 /dev/nvme3n1; do
        [ -b "$candidate" ] && DEV="$candidate" && break
      done
      [ -z "$DEV" ] && exit 0
      if ! blkid "$DEV" >/dev/null 2>&1; then
        mkfs.ext4 -F "$DEV"
      fi
      mkdir -p /mnt/data
      mountpoint -q /mnt/data || mount "$DEV" /mnt/data
      grep -q " /mnt/data " /etc/fstab || \
        echo "$DEV /mnt/data ext4 defaults,nofail 0 2" >> /etc/fstab
      chown root:root /mnt/data
      chmod 0755 /mnt/data
      # Point fibre state at the NVMe. celestia-appd state is redirected
      # inside validator_init.sh / encoder_init.sh where --home is
      # explicit and relative paths resolve through the mountpoint.
      mkdir -p /mnt/data/.celestia-fibre
      ln -sfn /mnt/data/.celestia-fibre /root/.celestia-fibre
runcmd:
  - hostnamectl set-hostname %s
  - /usr/local/sbin/talis-setup-nvme.sh
  - mkdir -p /root/.ssh
  - 'echo "%s" > /root/.ssh/authorized_keys'
  - chmod 700 /root/.ssh
  - chmod 600 /root/.ssh/authorized_keys
  - chown -R root:root /root/.ssh
`,
		instanceName,
		instanceName,
		instanceName,
		strings.ReplaceAll(key, `"`, `\"`),
	)
}
