package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

const (
	LinodeDefaultValidatorSlug = "g6-standard-8"
	LinodeDefaultImage         = "linode/ubuntu-22.04"
)

var LinodeRegions = []string{
	"us-central",
	"us-west",
	"us-southeast",
	"us-east",
	"ap-south",
	"ap-northeast",
	"ap-west",
	"ca-central",
	"eu-west",
	"eu-central",
}

func NewLinodeValidator(region string) Instance {
	if region == "" || region == RandomRegion {
		region = RandomLinodeRegion()
	}
	i := NewBaseInstance(Validator)
	i.Provider = Linode
	i.Slug = LinodeDefaultValidatorSlug
	i.Region = region
	return i
}

func RandomLinodeRegion() string {
	return LinodeRegions[rand.Intn(len(LinodeRegions))]
}

func LinodeClient(token string) *linodego.Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	linodeClient := linodego.NewClient(oauth2Client)
	linodeClient.SetDebug(true)
	return &linodeClient
}

func GetLinodeSSHKeyMeta(ctx context.Context, client *linodego.Client, publicKey string) (*linodego.SSHKey, error) {
	publicKey = strings.TrimSpace(publicKey)

	keys, err := client.ListSSHKeys(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list SSH keys: %w", err)
	}

	for _, key := range keys {
		if strings.TrimSpace(key.SSHKey) == publicKey {
			return &key, nil
		}
	}

	return nil, fmt.Errorf(
		"ssh public key not found in Linode. Please upload it via the control panel or API before proceeding",
	)
}

func CreateLinodes(ctx context.Context, client *linodego.Client, insts []Instance, key *linodego.SSHKey) ([]Instance, error) {
	total := len(insts)

	type result struct {
		inst         Instance
		err          error
		timeRequired time.Duration
	}

	results := make(chan result, total)
	workers := make(chan struct{}, 10)
	var wg sync.WaitGroup
	wg.Add(total)

	for _, v := range insts {
		go func(v Instance) {
			workers <- struct{}{}
			defer func() {
				<-workers
				wg.Done()
			}()

			ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
			defer cancel()

			createOpts := linodego.InstanceCreateOptions{
				Region:         v.Region,
				Type:           v.Slug,
				Image:          LinodeDefaultImage,
				Label:          v.Name,
				Tags:           v.Tags,
				AuthorizedKeys: []string{key.SSHKey},
			}

			start := time.Now()

			log.Println("Creating linode instance", v.Name, "in region", v.Region, start.Format(time.RFC3339))

			instance, err := client.CreateInstance(ctx, createOpts)
			if err != nil {
				results <- result{inst: v, err: fmt.Errorf("create %s: %w", v.Name, err)}
				return
			}

			// get the public ip address
			if len(instance.IPv4) == 0 {
				results <- result{inst: v, err: fmt.Errorf("no ip address found for instance %s", v.Name)}
				return
			}

			v.PublicIP = instance.IPv4[0].String()

			results <- result{inst: v, err: nil, timeRequired: time.Since(start)}
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
			fmt.Printf("✅ %s is up (public=%s) in %v\n",
				res.inst.Name, res.inst.PublicIP, res.timeRequired)
		}
		fmt.Printf("---- Progress: %d/%d\n", len(created), total)
	}

	return created, nil
}

func DestroyLinodes(ctx context.Context, client *linodego.Client, insts []Instance) ([]Instance, error) {
	total := len(insts)

	type result struct {
		inst         Instance
		err          error
		timeRequired time.Duration
	}

	results := make(chan result, total)
	workers := make(chan struct{}, 10)
	var wg sync.WaitGroup
	wg.Add(total)

	for _, v := range insts {
		go func(inst Instance) {
			workers <- struct{}{}
			defer func() {
				<-workers
				wg.Done()
			}()
			start := time.Now()

			fmt.Println("⏳ Deleting linode instance", inst.Name, inst.PublicIP)

			delCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			instances, err := client.ListInstances(delCtx, &linodego.ListOptions{
				Filter: fmt.Sprintf("{\"label\":\"%s\"}", inst.Name),
			})
			if err != nil {
				results <- result{inst: inst, err: fmt.Errorf("failed to list instances: %w", err)}
				return
			}

			if len(instances) == 0 {
				results <- result{inst: inst, err: fmt.Errorf("no instances found with name %s", inst.Name)}
				return
			}

			for _, instance := range instances {
				err := client.DeleteInstance(delCtx, instance.ID)
				if err != nil {
					results <- result{inst: inst, err: fmt.Errorf("delete %s: %w", inst.Name, err)}
					return
				}
			}

			results <- result{inst: inst, err: nil, timeRequired: time.Since(start)}
		}(v)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var removed []Instance
	var failed []result
	for res := range results {
		if res.err != nil {
			fmt.Printf("❌ %s failed to delete after %v: %v\n",
				res.inst.Name, res.timeRequired, res.err)
			failed = append(failed, res)
		} else {
			removed = append(removed, res.inst)
			fmt.Printf("✅ %s deleted (took %v)\n", res.inst.Name, res.timeRequired)
		}
		fmt.Printf("---- Progress: %d/%d\n", len(removed)+len(failed), total)
	}

	return removed, nil
}
