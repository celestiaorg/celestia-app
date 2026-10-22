// Command review-policy publishes the review-policy commit status. It runs
// only from the protected default branch, never from pull request code, and
// uses only the standard library so the privileged job downloads nothing.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	statusContext  = "review-policy"
	eventsWorkflow = ".github/workflows/review-policy-events.yml"
)

type User struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	User   User   `json:"user"`
	Head   Ref    `json:"head"`
	Base   Ref    `json:"base"`
}

type Review struct {
	ID       int64  `json:"id"`
	User     User   `json:"user"`
	State    string `json:"state"`
	CommitID string `json:"commit_id"`
}

type WorkflowRun struct {
	Path           string `json:"path"`
	Event          string `json:"event"`
	HeadBranch     string `json:"head_branch"`
	HeadSHA        string `json:"head_sha"`
	Conclusion     string `json:"conclusion"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

type Event struct {
	Repository struct {
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	PullRequest *PullRequest `json:"pull_request"`
	WorkflowRun *WorkflowRun `json:"workflow_run"`
}

// API is the subset of the GitHub REST API the policy uses.
type API interface {
	Get(path string, out any) error
	Post(path string, body any) error
}

type client struct {
	http  *http.Client
	root  string
	token string
}

func (c *client) do(method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.root+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GitHub API %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) Get(path string, out any) error   { return c.do(http.MethodGet, path, nil, out) }
func (c *client) Post(path string, body any) error { return c.do(http.MethodPost, path, body, nil) }

// list fetches every page of a list endpoint.
func list[T any](api API, path string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var items []T
	for page := 1; ; page++ {
		var batch []T
		if err := api.Get(fmt.Sprintf("%s%sper_page=100&page=%d", path, sep, page), &batch); err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) < 100 {
			return items, nil
		}
	}
}

type runner struct {
	api           API
	repository    string
	defaultBranch string
	targetURL     string
	report        func(string)
	permissions   map[string]bool
}

type checked struct {
	Result
	sha string
}

func (r *runner) canWrite(login string) (bool, error) {
	key := strings.ToLower(login)
	if ok, found := r.permissions[key]; found {
		return ok, nil
	}
	var data struct {
		Permission string `json:"permission"`
	}
	if err := r.api.Get("/collaborators/"+url.PathEscape(login)+"/permission", &data); err != nil {
		return false, err
	}
	ok := slices.Contains([]string{"admin", "maintain", "write"}, data.Permission)
	r.permissions[key] = ok
	return ok, nil
}

func (r *runner) status(sha, state, description string) error {
	if d := []rune(description); len(d) > 140 {
		description = string(d[:140])
	}
	return r.api.Post("/statuses/"+sha, map[string]string{
		"context": statusContext, "state": state, "description": description, "target_url": r.targetURL,
	})
}

func outcome(ok bool) string {
	if ok {
		return "success"
	}
	return "failure"
}

func (r *runner) openPulls() ([]PullRequest, error) {
	return list[PullRequest](r.api, "/pulls?state=open&base="+url.QueryEscape(r.defaultBranch))
}

// check evaluates one PR. It returns nil for PRs the policy does not govern.
// Evaluation errors fail closed instead of being returned.
func (r *runner) check(number int, publish bool) (*checked, error) {
	var pr PullRequest
	if err := r.api.Get(fmt.Sprintf("/pulls/%d", number), &pr); err != nil {
		return nil, err
	}
	if pr.State != "open" || pr.Base.Ref != r.defaultBranch {
		return nil, nil
	}
	if publish {
		if err := r.status(pr.Head.SHA, "pending", "Evaluating independent approvals"); err != nil {
			return nil, err
		}
	}
	res, err := r.evaluatePR(pr)
	if err != nil {
		r.report(fmt.Sprintf("#%d: %v", number, err))
		if publish {
			return &checked{sha: pr.Head.SHA}, r.status(pr.Head.SHA, "failure", err.Error())
		}
		return &checked{sha: pr.Head.SHA}, nil
	}
	r.report(fmt.Sprintf("#%d: %s", number, res.Summary))
	if publish {
		if err := r.status(pr.Head.SHA, outcome(res.OK), res.Summary); err != nil {
			return nil, err
		}
	}
	return &checked{Result: res, sha: pr.Head.SHA}, nil
}

func (r *runner) evaluatePR(pr PullRequest) (Result, error) {
	reviews, err := list[Review](r.api, fmt.Sprintf("/pulls/%d/reviews", pr.Number))
	if err != nil {
		return Result{}, err
	}
	res, err := evaluate(pr, reviews, r.canWrite)
	if err != nil {
		return Result{}, err
	}
	var fresh PullRequest
	if err := r.api.Get(fmt.Sprintf("/pulls/%d", pr.Number), &fresh); err != nil {
		return Result{}, err
	}
	if fresh.Head.SHA != pr.Head.SHA || fresh.Base.SHA != pr.Base.SHA || fresh.Title != pr.Title ||
		fresh.Draft != pr.Draft || fresh.State != "open" {
		return Result{}, errors.New("PR changed during evaluation; rerun review-policy")
	}
	return res, nil
}

// checkHeads publishes one status per head commit. Statuses belong to
// commits, not PRs, so every PR sharing a commit must pass for it to pass.
func (r *runner) checkHeads(pulls []PullRequest) error {
	var shas []string
	groups := map[string][]PullRequest{}
	for _, pr := range pulls {
		if _, found := groups[pr.Head.SHA]; !found {
			shas = append(shas, pr.Head.SHA)
		}
		groups[pr.Head.SHA] = append(groups[pr.Head.SHA], pr)
	}
	for _, sha := range shas {
		if err := r.status(sha, "pending", "Evaluating independent approvals"); err != nil {
			return err
		}
		group := groups[sha]
		ok := true
		var summary string
		for _, pr := range group {
			c, err := r.check(pr.Number, false)
			if err != nil {
				return err
			}
			ok = ok && c != nil && c.OK && c.sha == sha
			if c != nil {
				summary = c.Summary
			}
		}
		if len(group) > 1 || summary == "" {
			summary = fmt.Sprintf("%d PR(s) sharing this commit: ", len(group))
			if ok {
				summary += "passed"
			} else {
				summary += "review required"
			}
		}
		if err := r.status(sha, outcome(ok), summary); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) refresh(number int) error {
	var pr PullRequest
	if err := r.api.Get(fmt.Sprintf("/pulls/%d", number), &pr); err != nil {
		return err
	}
	if pr.State != "open" || pr.Base.Ref != r.defaultBranch {
		return nil
	}
	pulls, err := r.openPulls()
	if err != nil {
		return err
	}
	var same []PullRequest
	for _, p := range pulls {
		if p.Head.SHA == pr.Head.SHA {
			same = append(same, p)
		}
	}
	return r.checkHeads(same)
}

// mergeGroup revalidates every queued PR against its current reviews and
// publishes the result on the merge-group commit.
func (r *runner) mergeGroup(run *WorkflowRun) error {
	if run.HeadRepository.FullName != r.repository ||
		!strings.HasPrefix(run.HeadBranch, "gh-readonly-queue/"+r.defaultBranch+"/pr-") {
		return nil
	}
	sha := run.HeadSHA
	if err := r.status(sha, "pending", "Revalidating every queued PR against current reviews"); err != nil {
		return err
	}
	n, err := r.queuedPulls(run)
	if err != nil {
		return r.status(sha, "failure", err.Error())
	}
	return r.status(sha, "success", fmt.Sprintf("%d queued PR(s) satisfy review policy", n))
}

func (r *runner) queuedPulls(run *WorkflowRun) (int, error) {
	if run.Conclusion != "success" {
		return 0, errors.New("merge-group policy tests did not pass")
	}
	candidates, err := r.openPulls()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, pr := range candidates {
		var cmp struct {
			Status string `json:"status"`
		}
		if err := r.api.Get(fmt.Sprintf("/compare/%s...%s", pr.Head.SHA, run.HeadSHA), &cmp); err != nil {
			return 0, err
		}
		if cmp.Status != "ahead" && cmp.Status != "identical" {
			continue
		}
		n++
		c, err := r.check(pr.Number, false)
		if err != nil {
			return 0, err
		}
		if c == nil || !c.OK || c.sha != pr.Head.SHA {
			return 0, fmt.Errorf("PR #%d no longer satisfies review policy", pr.Number)
		}
	}
	if n == 0 {
		return 0, errors.New("cannot identify PRs in merge group")
	}
	return n, nil
}

func (r *runner) handle(eventName string, event Event) error {
	switch eventName {
	case "pull_request_target":
		if event.PullRequest == nil {
			return errors.New("pull_request_target event without a pull request")
		}
		return r.refresh(event.PullRequest.Number)
	case "workflow_run":
		run := event.WorkflowRun
		if run == nil || run.Path != eventsWorkflow {
			return nil
		}
		if run.Event == "merge_group" {
			return r.mergeGroup(run)
		}
		if run.Event != "pull_request_review" {
			return nil
		}
		// Fork review events carry no PR numbers, so sweep every open PR.
		if len(run.PullRequests) == 0 {
			return r.sweep()
		}
		// Never trust artifacts or PR-provided output from the read-only notifier.
		for _, pr := range run.PullRequests {
			if err := r.refresh(pr.Number); err != nil {
				return err
			}
		}
		return nil
	case "schedule", "workflow_dispatch":
		return r.sweep()
	default:
		return fmt.Errorf("unsupported event: %s", eventName)
	}
}

// sweep reconciles missed events and permission changes.
func (r *runner) sweep() error {
	pulls, err := r.openPulls()
	if err != nil {
		return err
	}
	return r.checkHeads(pulls)
}

func run() error {
	data, err := os.ReadFile(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	repository := os.Getenv("GITHUB_REPOSITORY")
	r := &runner{
		api: &client{
			http:  &http.Client{Timeout: 30 * time.Second},
			root:  os.Getenv("GITHUB_API_URL") + "/repos/" + repository,
			token: os.Getenv("GITHUB_TOKEN"),
		},
		repository:    repository,
		defaultBranch: event.Repository.DefaultBranch,
		targetURL:     fmt.Sprintf("%s/%s/actions/runs/%s", os.Getenv("GITHUB_SERVER_URL"), repository, os.Getenv("GITHUB_RUN_ID")),
		report:        func(s string) { fmt.Println(s) },
		permissions:   map[string]bool{},
	}
	return r.handle(os.Getenv("GITHUB_EVENT_NAME"), event)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
