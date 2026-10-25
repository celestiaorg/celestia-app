package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Risk is the review tier of a pull request.
type Risk int

const (
	Low Risk = iota
	Medium
	High
)

func (r Risk) String() string { return [...]string{"low", "medium", "high"}[r] }

// Approvals returns the number of independent human approvals the tier requires.
func (r Risk) Approvals() int { return [...]int{0, 0, 1}[r] }

var titleTag = regexp.MustCompile(`^[a-z]+(\([^\r\n)]+\))?!?: \[(low|medium|high)\] \S`)

// classify returns the tier named by the PR title's [low], [medium], or [high] tag.
func classify(title string) (Risk, error) {
	tag := titleTag.FindStringSubmatch(title)
	if tag == nil {
		return 0, errors.New("PR title must contain a conventional-commit [low], [medium], or [high] tag")
	}
	return Risk(slices.Index([]string{"low", "medium", "high"}, tag[2])), nil
}

// Result is the outcome of evaluating one pull request.
type Result struct {
	OK       bool
	Risk     Risk
	Required int
	Approved []string
	Blocking []string
	Summary  string
}

func human(u User) bool { return u.Type == "User" && !strings.HasSuffix(u.Login, "[bot]") }

// evaluate applies the review policy to a pull request snapshot. canWrite
// reports whether a login has write, maintain, or admin permission.
func evaluate(pr PullRequest, reviews []Review, canWrite func(string) (bool, error)) (Result, error) {
	risk, err := classify(pr.Title)
	if err != nil {
		return Result{}, err
	}
	author := strings.ToLower(pr.User.Login)
	trusted := false
	if human(pr.User) {
		if trusted, err = canWrite(author); err != nil {
			return Result{}, err
		}
	}
	// Bot-authored and external PRs always need one maintainer. For agent PRs,
	// the maintainer who prompted the agent may be that approver.
	required := risk.Approvals()
	if !trusted {
		required = max(required, 1)
	}

	latest := map[string]Review{}
	sorted := slices.Clone(reviews)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, r := range sorted {
		if !human(r.User) || r.State == "COMMENTED" || r.State == "PENDING" {
			continue
		}
		latest[strings.ToLower(r.User.Login)] = r
	}
	logins := make([]string, 0, len(latest))
	for login := range latest {
		logins = append(logins, login)
	}
	sort.Strings(logins)

	res := Result{Risk: risk, Required: required}
	for _, login := range logins {
		if login == author {
			continue
		}
		ok, err := canWrite(login)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		switch r := latest[login]; {
		case r.State == "CHANGES_REQUESTED":
			res.Blocking = append(res.Blocking, login)
		case r.State == "APPROVED" && r.CommitID == pr.Head.SHA:
			res.Approved = append(res.Approved, login)
		}
	}
	res.OK = !pr.Draft && len(res.Blocking) == 0 && len(res.Approved) >= required
	res.Summary = fmt.Sprintf("%s: %d/%d independent approvals", risk, len(res.Approved), required)
	if pr.Draft {
		res.Summary += "; draft"
	}
	if len(res.Blocking) > 0 {
		res.Summary += "; changes requested by " + strings.Join(res.Blocking, ", ")
	}
	return res, nil
}
