package pr

import "testing"

func TestParseGHOutput(t *testing.T) {
	sample := []byte(`[
		{"number":42,"headRefName":"feat-auth","state":"OPEN","isDraft":false,"reviewDecision":"APPROVED","statusCheckRollup":[{"conclusion":"SUCCESS"}],"url":"https://github.com/x/y/pull/42"},
		{"number":43,"headRefName":"feat-old","state":"MERGED","isDraft":false,"reviewDecision":"","statusCheckRollup":[],"url":"https://github.com/x/y/pull/43"},
		{"number":44,"headRefName":"feat-wip","state":"OPEN","isDraft":true,"reviewDecision":"","statusCheckRollup":[{"conclusion":"FAILURE"}],"url":"https://github.com/x/y/pull/44"},
		{"number":45,"headRefName":"feat-review","state":"OPEN","isDraft":false,"reviewDecision":"CHANGES_REQUESTED","statusCheckRollup":[{"status":"IN_PROGRESS"}],"url":"https://github.com/x/y/pull/45"}
	]`)
	byBranch, err := ParseGHOutput(sample)
	if err != nil {
		t.Fatalf("ParseGHOutput: %v", err)
	}
	if byBranch["feat-auth"].Display != "approved" || byBranch["feat-auth"].CIMark != "✓" {
		t.Fatalf("unexpected feat-auth status: %+v", byBranch["feat-auth"])
	}
	if !byBranch["feat-old"].IsMerged || byBranch["feat-old"].Display != "merged" {
		t.Fatalf("unexpected feat-old status: %+v", byBranch["feat-old"])
	}
	if byBranch["feat-wip"].Display != "draft" || byBranch["feat-wip"].CIMark != "✗" {
		t.Fatalf("unexpected feat-wip status: %+v", byBranch["feat-wip"])
	}
	if byBranch["feat-review"].Display != "changes requested" || byBranch["feat-review"].CIMark != "…" {
		t.Fatalf("unexpected feat-review status: %+v", byBranch["feat-review"])
	}
}
