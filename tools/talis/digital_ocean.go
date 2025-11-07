package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/digitalocean/godo"
)

const (
	DODefaultValidatorSlug = "c2-16vcpu-32gb"
	DODefaultImage         = "ubuntu-22-04-x64"
	RandomRegion           = "random"
)

var (
	DORegions = []string{
		"nyc1", "nyc3", "tor1", "sfo2", "sfo3", "ams3", "sgp1", "lon1", "fra1", "syd1",
	}

	DOLargeRegions = map[string]int{
		"nyc3": 6, "tor1": 6, "sfo2": 2, "sfo3": 6, "ams3": 8, "sgp1": 4, "lon1": 8, "fra1": 6, "syd1": 6,
	}

	DOMediumRegions = map[string]int{
		"nyc3": 2, "tor1": 2, "sfo3": 2, "ams3": 2, "lon1": 2,
	}

	DOSmallRegions = map[string]int{
		"ams3": 1, "tor1": 1, "nyc3": 1, "lon1": 1,
	}
)

func NewDigitalOceanValidator(region string) Instance {
	if region == "" || region == RandomRegion {
		region = RandomDORegion()
	}
	i := NewBaseInstance(Validator)
	i.Provider = DigitalOcean
	i.Slug = DODefaultValidatorSlug
	i.Region = region
	return i
}

func RandomDORegion() string {
	return DORegions[rand.Intn(len(DORegions))]
}

// GetDOSSHKeyMeta checks if the provided raw SSH public key is registered in DigitalOcean
// and returns its ID and Name. If not found, returns an error instructing to upload the key.
func GetDOSSHKeyMeta(ctx context.Context, client *godo.Client, publicKey string) (godo.Key, error) {
	pubKeySplit := strings.Split(publicKey, " ")
	if len(pubKeySplit) <= 1 {
		return godo.Key{}, fmt.Errorf("invalid public key format")
	}
	publicKey = strings.Join(pubKeySplit[:2], "")

	// Pagination options
	opt := &godo.ListOptions{PerPage: 200}

	for {
		keys, resp, err := client.Keys.List(ctx, opt)
		if err != nil {
			return godo.Key{}, fmt.Errorf("failed to list SSH keys: %w", err)
		}

		for _, key := range keys {
			// only compare the first two parts of the public key. The third part is the host
			// which can be ignored.
			if strings.Join(strings.Split(key.PublicKey, " ")[:2], "") == publicKey {
				return key, nil
			}
		}

		// Break if we're at the last page
		if resp.Links.IsLastPage() {
			break
		}
		// Advance to next page
		page, err := resp.Links.CurrentPage()
		if err != nil {
			return godo.Key{}, fmt.Errorf("unable to parse pagination: %w", err)
		}
		opt.Page = page + 1
	}

	return godo.Key{}, fmt.Errorf(
		"ssh public key not found in DigitalOcean. Please upload it via the control panel or API before proceeding",
	)
}

// CreateDroplets launches all droplets in parallel, waits for their IPs, and
// returns the filled-out []Instance slice.
func CreateDroplets(ctx context.Context, client *godo.Client, insts []Instance, key godo.Key, workers int) ([]Instance, error) {
	total := len(insts)

	type result struct {
		inst         Instance
		err          error
		timeRequired time.Duration
	}

	insts, existing, err := filterExistingInstances(ctx, client, insts)
	if err != nil {
		return nil, err
	}

	if len(existing) > 0 {
		log.Println("Existing instances found, so they are not being created.")
		for _, v := range existing {
			log.Println("Skipping", v.Name, v.PublicIP, v.Tags)
		}
	}

	results := make(chan result, total)
	workerChan := make(chan struct{}, workers)
	var wg sync.WaitGroup
	wg.Add(total)

	for _, v := range insts {
		go func() {
			workerChan <- struct{}{}
			defer func() {
				<-workerChan
				wg.Done()
			}()

			ctx, cancel := context.WithTimeout(ctx, 7*time.Minute)
			defer cancel()

			req := &godo.DropletCreateRequest{
				Name:   v.Name,
				Region: v.Region,
				Size:   v.Slug,
				Image: godo.DropletCreateImage{
					Slug: "ubuntu-22-04-x64",
				},
				SSHKeys: []godo.DropletCreateSSHKey{{ID: key.ID, Fingerprint: key.Fingerprint}},
				Tags:    v.Tags,
			}

			start := time.Now()

			log.Println("Creating droplet", v.Name, "in region", v.Region, start.Format(time.RFC3339))

			d, _, err := client.Droplets.Create(ctx, req)
			if err != nil {
				results <- result{inst: v, err: fmt.Errorf("create %s: %w", v.Name, err)}
				return
			}

			pubIP, privIP, err := waitForNetworkIP(ctx, client, d.ID)
			if err != nil {
				results <- result{inst: v, err: fmt.Errorf("public IP %s: %w", v.Name, err)}
				return
			}

			v.PublicIP = pubIP
			v.PrivateIP = privIP
			results <- result{inst: v, err: nil, timeRequired: time.Since(start)}
		}()
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

func filterExistingInstances(ctx context.Context, client *godo.Client, insts []Instance) ([]Instance, []Instance, error) {
	droplets, err := listAllDroplets(ctx, client)
	if err != nil {
		return nil, nil, fmt.Errorf("listing before delete: %w", err)
	}

	var existing []Instance //nolint:prealloc
	var newInsts []Instance //nolint:prealloc
	for _, inst := range insts {
		var exists bool
		experimentTag := getExperimentTag(inst.Tags)
		if experimentTag == "" {
			newInsts = append(newInsts, inst)
			continue
		}
		for _, d := range droplets {
			if slices.Contains(d.Tags, experimentTag) {
				exists = true
				break
			}
		}

		if !exists {
			newInsts = append(newInsts, inst)
			continue
		}

		existing = append(existing, inst)
	}

	return newInsts, existing, nil
}

func getExperimentTag(tags []string) string {
	for _, tag := range tags {
		if strings.Contains(tag, "validator-") || strings.Contains(tag, "bridge-") || strings.Contains(tag, "light-") {
			return tag
		}
	}
	return ""
}

// waitForNetworkIP polls until the droplet has an IPv4 of the given type ("public" or "private")
// or ctx is done.
func waitForNetworkIP(ctx context.Context, client *godo.Client, dropletID int) (pub, priv string, err error) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-ticker.C:
			d, _, err := client.Droplets.Get(ctx, dropletID)
			if err != nil {
				return "", "", err
			}
			for _, net := range d.Networks.V4 {
				if net.Type == "public" {
					pub = net.IPAddress
				}
				if net.Type == "private" {
					priv = net.IPAddress
				}
				if pub != "" && priv != "" {
					return pub, priv, nil
				}
			}
		}
	}
}

func DestroyDroplets(ctx context.Context, client *godo.Client, insts []Instance, workers int) ([]Instance, error) {
	droplets, err := listAllDroplets(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("listing droplets: %w", err)
	}

	return destroyDropletsByMatch(ctx, client, droplets, insts, workers, matchByExperimentTag)
}

func destroyAllTalisDroplets(ctx context.Context, client *godo.Client, workers int) ([]Instance, error) {
	droplets, err := listAllDroplets(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("listing droplets: %w", err)
	}

	var talisInstances []Instance
	for _, d := range droplets {
		if slices.Contains(d.Tags, "talis") {
			publicIP := ""
			for _, net := range d.Networks.V4 {
				if net.Type == "public" {
					publicIP = net.IPAddress
					break
				}
			}
			talisInstances = append(talisInstances, Instance{
				Name:     d.Name,
				PublicIP: publicIP,
			})
		}
	}

	if len(talisInstances) == 0 {
		log.Println("No talis droplets found to destroy")
		return nil, nil
	}

	return destroyDropletsByMatch(ctx, client, droplets, talisInstances, workers, matchByName)
}

type dropletMatcher func(inst Instance, d godo.Droplet) bool

func matchByExperimentTag(inst Instance, d godo.Droplet) bool {
	experimentTag := getExperimentTag(inst.Tags)
	return experimentTag != "" && slices.Contains(d.Tags, experimentTag)
}

func matchByName(inst Instance, d godo.Droplet) bool {
	return d.Name == inst.Name
}

func destroyDropletsByMatch(ctx context.Context, client *godo.Client, droplets []godo.Droplet, insts []Instance, workers int, matcher dropletMatcher) ([]Instance, error) {
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

			fmt.Println("⏳ Deleting droplet", inst.Name, inst.PublicIP)

			delCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			var matchIDs []int
			for _, d := range droplets {
				if matcher(inst, d) {
					matchIDs = append(matchIDs, d.ID)
				}
			}

			if len(matchIDs) > 1 {
				results <- result{inst: inst, err: fmt.Errorf("multiple droplets match %s", inst.Name)}
			}

			if len(matchIDs) == 0 {
				results <- result{inst: inst, err: fmt.Errorf("no droplets found for %s", inst.Name)}
				return
			}

			for _, id := range matchIDs {
				_, err := client.Droplets.Delete(delCtx, id)
				if err != nil {
					results <- result{inst: inst, err: fmt.Errorf("delete %s: %w", inst.Name, err)}
					return
				}

				if err := waitForDeletion(delCtx, client, id); err != nil {
					results <- result{inst: inst, err: fmt.Errorf("confirm delete %s: %w", inst.Name, err)}
					return
				}

				results <- result{inst: inst, err: nil, timeRequired: time.Since(start)}
			}
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
			fmt.Printf("❌ %s failed to delete after %v: %v\n",
				res.inst.Name, res.timeRequired, res.err)
			failed = append(failed, res)
		} else {
			removed = append(removed, res.inst)
			fmt.Printf("✅ %s deleted (took %v)\n", res.inst.Name, res.timeRequired)
		}
		fmt.Printf("---- Progress: %d/%d\n", len(removed)+len(failed), len(insts))
	}

	return removed, nil
}

// waitForDeletion polls until Get() returns a 404 Not Found or ctx is done.
func waitForDeletion(ctx context.Context, client *godo.Client, dropletID int) error {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, resp, err := client.Droplets.Get(ctx, dropletID)
			if err != nil {
				// godo returns a non-nil resp when it's an HTTP error
				if resp != nil && resp.StatusCode == http.StatusNotFound {
					return nil
				}
				// other errors: continue polling or exit?
				return err
			}
			// still exists; try again
		}
	}
}

// listAllDroplets pages through your account’s droplets.
func listAllDroplets(ctx context.Context, client *godo.Client) ([]godo.Droplet, error) {
	var all []godo.Droplet
	opt := &godo.ListOptions{PerPage: 200}
	for {
		page, resp, err := client.Droplets.List(ctx, opt)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		pageNum, _ := resp.Links.CurrentPage()
		opt.Page = pageNum + 1
	}
	return all, nil
}

// hasAllTags returns true if candidate contains every tag in want.
func hasAllTags(candidate, want []string) bool {
	tagset := make(map[string]struct{}, len(candidate))
	for _, t := range candidate {
		tagset[t] = struct{}{}
	}
	for _, w := range want {
		if _, ok := tagset[w]; !ok {
			return false
		}
	}
	return true
}

func checkForRunningDOExperiments(ctx context.Context, client *godo.Client) (bool, error) {
	if client == nil {
		return false, nil
	}

	droplets, err := listAllDroplets(ctx, client)
	if err != nil {
		return false, fmt.Errorf("failed to list droplets: %w", err)
	}

	for _, d := range droplets {
		if slices.Contains(d.Tags, "talis") && hasValidatorTag(d.Tags) {
			return true, nil
		}
	}

	return false, nil
}

func hasValidatorTag(tags []string) bool {
	for _, tag := range tags {
		if strings.HasPrefix(tag, "validator-") || strings.HasPrefix(tag, "bridge-") || strings.HasPrefix(tag, "light-") {
			return true
		}
	}
	return false
}
