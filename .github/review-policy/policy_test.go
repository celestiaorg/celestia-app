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
		Number: 1, Title: "docs: [low] clarify usage", State: "open", ChangedFiles: 1,
		User: person("author"), Head: Ref{SHA: sha}, Base: Ref{Ref: "main", SHA: strings.Repeat("b", 40)},
	}
}

func TestClassify(t *testing.T) {
	for _, path := range []string{
		"app/foo.go", "x/foo/keeper.go", "proto/foo.proto", "fibre/server.go", ".github/workflows/ci.yml",
		"go.mod", "test/e2e/go.mod", "docs/ai/invariants.md", "AGENTS.md", "x/blob/REVIEW.md",
		"specs/src/protocol.md", "docs/diagram.png", "new-production-dir/main.go",
	} {
		if got, _ := classify("docs: [low] x", []File{{Filename: path}}); got != High {
			t.Errorf("%s: got %s, want high", path, got)
		}
	}
	tests := []struct {
		title string
		files []File
		want  Risk
	}{
		{"docs: [low] x", []File{{Filename: "docs/usage.md"}, {Filename: "README.md"}}, Low},
		{"test: [low] x", []File{{Filename: "app/foo_test.go"}}, Medium},
		{"test: [low] x", []File{{Filename: "test/util/helpers.go"}}, Medium},
		{"docs: [high] new protocol", []File{{Filename: "docs/usage.md"}}, High},
		{"fix(api)!: [medium] x", []File{{Filename: "docs/usage.md"}}, Medium},
		{"docs: [low] x", []File{{Filename: "docs/foo.md", PreviousFilename: "app/foo.go"}}, High},
	}
	for _, tc := range tests {
		got, err := classify(tc.title, tc.files)
		if err != nil || got != tc.want {
			t.Errorf("classify(%q, %v) = %s, %v; want %s", tc.title, tc.files, got, err, tc.want)
		}
	}
	if _, err := classify("docs: missing tag", []File{{Filename: "README.md"}}); err == nil {
		t.Error("untagged title should fail")
	}
	if _, err := classify("docs: [low] x", nil); err == nil {
		t.Error("empty diff should fail")
	}
}

func TestEvaluate(t *testing.T) {
	medium := []File{{Filename: "app/handler_test.go"}}
	high := []File{{Filename: "app/handler.go"}}
	writers := func(login string) (bool, error) { return login != "reader" && login != "outsider", nil }
	tests := []struct {
		name     string
		pr       func(*PullRequest)
		files    []File
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
		{name: "medium needs one approval", files: medium},
		{name: "medium with one approval", files: medium, reviews: []Review{approve("alice", 1)}, ok: true, approved: []string{"alice"}},
		{name: "high counts each reviewer once", files: high, reviews: []Review{approve("alice", 1), approve("Alice", 2)}, approved: []string{"alice"}},
		{
			name: "high with two distinct approvals", files: high, reviews: []Review{approve("bob", 2), approve("alice", 1)},
			ok: true, approved: []string{"alice", "bob"},
		},
		{
			name: "bots, readers, and stale approvals do not count", files: medium,
			reviews: []Review{
				{ID: 1, User: agent, State: "APPROVED", CommitID: sha},
				approve("reader", 2),
				{ID: 3, User: person("alice"), State: "APPROVED", CommitID: "old"},
			},
		},
		{
			name: "dismissal erases approval", files: medium,
			reviews: []Review{approve("alice", 1), {ID: 2, User: person("alice"), State: "DISMISSED", CommitID: sha}},
		},
		{
			name: "comment does not erase approval", files: medium,
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
			files := tc.files
			if files == nil {
				files = []File{{Filename: "docs/usage.md"}}
			}
			res, err := evaluate(pr, files, tc.reviews, writers)
			if err != nil {
				t.Fatal(err)
			}
			if res.OK != tc.ok || !slices.Equal(res.Approved, tc.approved) {
				t.Errorf("got ok=%v approved=%v (%s); want ok=%v approved=%v", res.OK, res.Approved, res.Summary, tc.ok, tc.approved)
			}
		})
	}
}
