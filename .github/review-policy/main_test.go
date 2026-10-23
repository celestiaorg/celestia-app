package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type status struct {
	sha, state, description string
}

// fakeAPI serves canned GitHub responses and records published statuses.
type fakeAPI struct {
	pulls      []PullRequest
	reviews    []Review
	comparison string
	mutate     bool
	reads      int
	statuses   []status
}

var pullPath = regexp.MustCompile(`^/pulls/(\d+)$`)

func (f *fakeAPI) Get(path string, out any) error {
	var v any
	switch {
	case strings.Contains(path, "page=") && !strings.HasSuffix(path, "page=1"):
		v = []any{}
	case strings.HasSuffix(path, "/permission"):
		v = map[string]string{"permission": "write"}
	case strings.HasPrefix(path, "/compare/"):
		v = map[string]string{"status": f.comparison}
	case strings.HasPrefix(path, "/pulls?"):
		v = f.pulls
	case strings.Contains(path, "/reviews?"):
		v = f.reviews
	case pullPath.MatchString(path):
		number, _ := strconv.Atoi(pullPath.FindStringSubmatch(path)[1])
		for _, pr := range f.pulls {
			if pr.Number == number {
				f.reads++
				if f.mutate && f.reads > 2 {
					pr.Head.SHA = strings.Repeat("c", 40)
				}
				v = pr
			}
		}
	default:
		return fmt.Errorf("unexpected path %s", path)
	}
	data, _ := json.Marshal(v)
	return json.Unmarshal(data, out)
}

func (f *fakeAPI) Post(path string, body any) error {
	b := body.(map[string]string)
	f.statuses = append(f.statuses, status{strings.TrimPrefix(path, "/statuses/"), b["state"], b["description"]})
	return nil
}

func (f *fakeAPI) last() status { return f.statuses[len(f.statuses)-1] }

func newFake() *fakeAPI {
	return &fakeAPI{pulls: []PullRequest{basePR()}, comparison: "ahead"}
}

func invoke(t *testing.T, f *fakeAPI, eventName string, event Event) {
	t.Helper()
	r := &runner{
		api: f, repository: "celestiaorg/celestia-app", defaultBranch: "main",
		report: func(string) {}, permissions: map[string]bool{},
	}
	if err := r.handle(eventName, event); err != nil {
		t.Fatal(err)
	}
}

func prEvent() Event {
	pr := basePR()
	return Event{PullRequest: &pr}
}

func queueRun() *WorkflowRun {
	run := &WorkflowRun{
		Path: eventsWorkflow, Event: "merge_group", HeadBranch: "gh-readonly-queue/main/pr-1-abc",
		HeadSHA: strings.Repeat("d", 40), Conclusion: "success",
	}
	run.HeadRepository.FullName = "celestiaorg/celestia-app"
	return run
}

func TestPullRequestStatus(t *testing.T) {
	f := newFake()
	invoke(t, f, "pull_request_target", prEvent())
	if got := f.last(); got.sha != sha || got.state != "success" {
		t.Errorf("got %+v, want success on head", got)
	}
}

func TestHeadStatusFailsClosed(t *testing.T) {
	untagged := newFake()
	untagged.pulls[0].Title = "docs: missing tag"
	mutated := newFake()
	mutated.mutate = true
	for name, f := range map[string]*fakeAPI{"untagged title": untagged, "PR changed": mutated} {
		invoke(t, f, "pull_request_target", prEvent())
		if got := f.last().state; got != "failure" {
			t.Errorf("%s: got %s, want failure", name, got)
		}
	}
}

func TestSharedCommitUsesStrictestResult(t *testing.T) {
	f := newFake()
	other := basePR()
	other.Number, other.Title = 2, "docs: [high] protocol note"
	f.pulls = append(f.pulls, other)
	invoke(t, f, "pull_request_target", prEvent())
	if len(f.statuses) != 2 || f.statuses[0].state != "pending" || f.statuses[1].state != "failure" {
		t.Errorf("got %+v, want pending then failure", f.statuses)
	}
}

func TestMergeGroup(t *testing.T) {
	failedTests := queueRun()
	failedTests.Conclusion = "failure"
	tests := []struct {
		name  string
		setup func(*fakeAPI)
		run   *WorkflowRun
		want  string
	}{
		{name: "queued PR still satisfies policy", run: queueRun(), want: "success"},
		{name: "queued PR now needs review", setup: func(f *fakeAPI) { f.pulls[0].Title = "docs: [high] policy" }, run: queueRun(), want: "failure"},
		{name: "unidentifiable group", setup: func(f *fakeAPI) { f.comparison = "diverged" }, run: queueRun(), want: "failure"},
		{name: "failed policy tests", run: failedTests, want: "failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			if tc.setup != nil {
				tc.setup(f)
			}
			invoke(t, f, "workflow_run", Event{WorkflowRun: tc.run})
			if got := f.last(); got.sha != tc.run.HeadSHA || got.state != tc.want {
				t.Errorf("got %+v, want %s on merge-group commit", got, tc.want)
			}
		})
	}
}

func TestWorkflowRunFiltering(t *testing.T) {
	unrelated := queueRun()
	unrelated.Path = ".github/workflows/attacker.yml"
	f := newFake()
	invoke(t, f, "workflow_run", Event{WorkflowRun: unrelated})
	if len(f.statuses) != 0 {
		t.Errorf("unrelated workflow published %+v", f.statuses)
	}

	// Fork review events have no PR numbers and fall back to a sweep.
	fork := queueRun()
	fork.Event, fork.HeadRepository.FullName = "pull_request_review", "contributor/fork"
	f = newFake()
	invoke(t, f, "workflow_run", Event{WorkflowRun: fork})
	if got := f.last(); got.sha != sha || got.state != "success" {
		t.Errorf("fork sweep: got %+v", got)
	}

	// A dismissed approval refreshes the PR named by the review event.
	review := queueRun()
	review.Event = "pull_request_review"
	review.PullRequests = append(review.PullRequests, struct {
		Number int `json:"number"`
	}{1})
	f = newFake()
	f.pulls[0].Title = "docs: [high] update"
	f.reviews = []Review{approve("alice", 1), {ID: 2, User: person("alice"), State: "DISMISSED", CommitID: sha}}
	invoke(t, f, "workflow_run", Event{WorkflowRun: review})
	if got := f.last().state; got != "failure" {
		t.Errorf("dismissal: got %s, want failure", got)
	}
}

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/repos/o/r/forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		items := []int{100}
		if r.URL.Query().Get("page") == "1" {
			items = make([]int, 100)
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()
	c := &client{http: srv.Client(), root: srv.URL + "/repos/o/r", token: "token"}
	items, err := list[int](c, "/pulls?state=open")
	if err != nil || len(items) != 101 {
		t.Errorf("got %d items, %v; want 101", len(items), err)
	}
	if err := c.Get("/forbidden", &items); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("got %v, want HTTP 403 error", err)
	}
}
