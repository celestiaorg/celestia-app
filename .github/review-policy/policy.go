package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Risk is the review tier of a pull request. Its value is the number of
// independent human approvals the tier requires.
type Risk int

const (
	Low Risk = iota
	Medium
	High
)

func (r Risk) String() string { return [...]string{"low", "medium", "high"}[r] }

var (
	// Review instructions and protocol specifications are policy, not ordinary docs.
	protectedPath   = regexp.MustCompile(`^(docs/(ai|plans)/|specs/|\.github/|\.agents/|\.claude/|\.cursor/)`)
	instructionFile = regexp.MustCompile(`(^|/)(AGENTS|CLAUDE|REVIEW)\.md$`)
	lowDoc          = regexp.MustCompile(`^(README|CONTRIBUTING|CHANGELOG)\.md$|^docs/.*\.md$`)
	goModule        = regexp.MustCompile(`(^|/)go\.(mod|sum)$`)
	testPath        = regexp.MustCompile(`_test\.go$|^test/`)
	titleTag        = regexp.MustCompile(`^[a-z]+(\([^\r\n)]+\))?!?: \[(low|medium|high)\] \S`)
)

// pathRisk returns the minimum tier for a change to path.
func pathRisk(path string) Risk {
	switch {
	case protectedPath.MatchString(path), instructionFile.MatchString(path):
		return High
	case lowDoc.MatchString(path):
		return Low
	case goModule.MatchString(path):
		return High
	case testPath.MatchString(path):
		return Medium
	default:
		// Unknown paths fail conservatively, including new production directories.
		return High
	}
}

// classify returns the higher of the title's tier and every changed path's floor.
func classify(title string, files []File) (Risk, error) {
	tag := titleTag.FindStringSubmatch(title)
	if tag == nil {
		return 0, errors.New("PR title must contain a conventional-commit [low], [medium], or [high] tag")
	}
	if len(files) == 0 {
		return 0, errors.New("cannot classify an empty or unavailable diff")
	}
	risk := Risk(slices.Index([]string{"low", "medium", "high"}, tag[2]))
	for _, f := range files {
		// A rename must satisfy the old and new path rules.
		for _, path := range []string{f.Filename, f.PreviousFilename} {
			if path != "" {
				risk = max(risk, pathRisk(path))
			}
		}
	}
	return risk, nil
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
func evaluate(pr PullRequest, files []File, reviews []Review, canWrite func(string) (bool, error)) (Result, error) {
	risk, err := classify(pr.Title, files)
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
	required := int(risk)
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
