package main

import (
	"slices"
	"strings"
	"testing"
)

var sha = strings.Repeat("a", 40)

func person(login string) User { return User{Login: login, Type: "User"} }

var agent = User{Login: "agent[bot]", Type: "Bot"}

func approve(login string, id int64) Review {
	return Review{ID: id, User: person(login), State: "APPROVED", CommitID: sha}
}

func basePR() PullRequest {
	return PullRequest{
		Number: 1, Title: "docs: [low] clarify usage", State: "open",
		User: person("author"), Head: Ref{SHA: sha}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)},
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		title string
		want  Risk
	}{
		{"docs: [low] clarify usage", Low},
		{"test: [medium] cover edge case", Medium},
		{"fix(api)!: [high] correct gas accounting", High},
		// The title alone sets the tier, whatever the diff touches.
		{"ci: [low] bump action", Low},
	}
	for _, tc := range tests {
		if got, err := classify(tc.title); err != nil || got != tc.want {
			t.Errorf("classify(%q) = %s, %v; want %s", tc.title, got, err, tc.want)
		}
	}
	for _, title := range []string{"docs: missing tag", "docs: [urgent] x", "[low] docs: x", "docs: [low]"} {
		if _, err := classify(title); err == nil {
			t.Errorf("classify(%q) should fail", title)
		}
	}
}

func TestEvaluate(t *testing.T) {
	const medium, high = "test: [medium] x", "fix: [high] x"
	writers := func(login string) (bool, error) { return login != "reader" && login != "outsider", nil }
	tests := []struct {
		name     string
		pr       func(*PullRequest)
		title    string
		reviews  []Review
		ok       bool
		approved []string
	}{
		{name: "trusted low author needs no reviewer", ok: true},
		{name: "drafts never pass", pr: func(p *PullRequest) { p.Draft = true }},
		{name: "agent PR needs one maintainer", pr: func(p *PullRequest) { p.User = agent }},
		{
			name: "prompting maintainer may approve agent PR", pr: func(p *PullRequest) { p.User = agent },
			reviews: []Review{approve("prompter", 1)}, ok: true, approved: []string{"prompter"},
		},
		{name: "outside author needs one maintainer", pr: func(p *PullRequest) { p.User = person("outsider") }},
		{name: "medium needs one approval", title: medium},
		{name: "medium with one approval", title: medium, reviews: []Review{approve("alice", 1)}, ok: true, approved: []string{"alice"}},
		{name: "high counts each reviewer once", title: high, reviews: []Review{approve("alice", 1), approve("Alice", 2)}, approved: []string{"alice"}},
		{
			name: "high with two distinct approvals", title: high, reviews: []Review{approve("bob", 2), approve("alice", 1)},
			ok: true, approved: []string{"alice", "bob"},
		},
		{
			name: "bots, readers, and stale approvals do not count", title: medium,
			reviews: []Review{
				{ID: 1, User: agent, State: "APPROVED", CommitID: sha},
				approve("reader", 2),
				{ID: 3, User: person("alice"), State: "APPROVED", CommitID: "old"},
			},
		},
		{
			name: "dismissal erases approval", title: medium,
			reviews: []Review{approve("alice", 1), {ID: 2, User: person("alice"), State: "DISMISSED", CommitID: sha}},
		},
		{
			name: "comment does not erase approval", title: medium,
			reviews: []Review{approve("alice", 1), {ID: 2, User: person("alice"), State: "COMMENTED", CommitID: sha}},
			ok:      true, approved: []string{"alice"},
		},
		{
			name: "outstanding change request blocks", reviews: []Review{{ID: 1, User: person("bob"), State: "CHANGES_REQUESTED", CommitID: "old"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pr := basePR()
			if tc.pr != nil {
				tc.pr(&pr)
			}
			if tc.title != "" {
				pr.Title = tc.title
			}
			res, err := evaluate(pr, tc.reviews, writers)
			if err != nil {
				t.Fatal(err)
			}
			if res.OK != tc.ok || !slices.Equal(res.Approved, tc.approved) {
				t.Errorf("got ok=%v approved=%v (%s); want ok=%v approved=%v", res.OK, res.Approved, res.Summary, tc.ok, tc.approved)
			}
		})
	}
}
